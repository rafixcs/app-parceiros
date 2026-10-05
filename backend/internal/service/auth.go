// Package service holds the use cases. Services depend only on the domain
// interfaces; the server package injects the infrastructure behind them.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	sessionTTL        = 30 * 24 * time.Hour
	sessionRefresh    = time.Hour
	verifyEmailTTL    = 7 * 24 * time.Hour
	resetPasswordTTL  = time.Hour
	minPasswordLength = 8
	maxPasswordLength = 128
	maxNameLength     = 80
	maxEmailLength    = 254
	tokenBytes        = 32
)

// Session is what a successful sign-in returns to the client.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// RegisterInput is the data of a new internal account.
type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

// InternalAuth is the internal identity provider: accounts with email and
// password, opaque session tokens and one-time tokens sent by email. It
// implements domain.Authenticator, so it can replace the external OIDC
// provider without touching the rest of the application.
type InternalAuth struct {
	repo   domain.AuthRepository
	hasher domain.PasswordHasher
	mailer domain.AuthMailer // nil: no emails are sent
	limits AuthLimits
	log    *slog.Logger
	now    func() time.Time
	// dummyHash is checked when the email is unknown, so a sign-in takes the
	// same time whether or not the account exists.
	dummyHash string
}

var _ domain.Authenticator = (*InternalAuth)(nil)

// AuthLimits throttle the attempts against the internal provider. Nil
// limiters do not throttle.
type AuthLimits struct {
	// Account limits sign-ins, sign-ups and emails per email address.
	Account domain.RateLimiter
	// IP limits sign-ins per client address, against password spraying.
	IP domain.RateLimiter
}

func NewInternalAuth(repo domain.AuthRepository, hasher domain.PasswordHasher, mailer domain.AuthMailer,
	limits AuthLimits, log *slog.Logger,
) (*InternalAuth, error) {
	dummy, err := hasher.Hash("not-a-real-password")
	if err != nil {
		return nil, err
	}
	return &InternalAuth{
		repo: repo, hasher: hasher, mailer: mailer, limits: limits, log: log,
		now: time.Now, dummyHash: dummy,
	}, nil
}

// Register creates an account, sends the email verification link and signs
// the user in.
func (s *InternalAuth) Register(ctx context.Context, in RegisterInput) (Session, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return Session{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return Session{}, domain.ErrInvalidName
	}
	if err := checkPassword(in.Password); err != nil {
		return Session{}, err
	}
	if err := s.allow(ctx, s.limits.Account, "register:"+email); err != nil {
		return Session{}, err
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return Session{}, err
	}
	acc, err := s.repo.CreateAccount(ctx, domain.AuthAccount{Email: email, Name: name, PasswordHash: hash})
	if err != nil {
		return Session{}, err
	}
	s.sendVerification(ctx, acc)
	return s.newSession(ctx, acc)
}

// Login checks email and password and opens a session. clientIP throttles
// guessing from one address across many accounts.
func (s *InternalAuth) Login(ctx context.Context, email, password, clientIP string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.allow(ctx, s.limits.Account, "login:"+email); err != nil {
		return Session{}, err
	}
	if clientIP != "" {
		if err := s.allow(ctx, s.limits.IP, "login:"+clientIP); err != nil {
			return Session{}, err
		}
	}
	acc, err := s.repo.AccountByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		_, _ = s.hasher.Verify(s.dummyHash, password)
		return Session{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	ok, err := s.hasher.Verify(acc.PasswordHash, password)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, domain.ErrInvalidCredentials
	}
	return s.newSession(ctx, acc)
}

// Logout ends the session of the token. An unknown token is not an error.
func (s *InternalAuth) Logout(ctx context.Context, token string) error {
	err := s.repo.DeleteSession(ctx, hashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// Authenticate resolves a session token into the identity of its account.
// Sessions slide: each use (at most once per hour) pushes the expiry forward.
func (s *InternalAuth) Authenticate(ctx context.Context, token string) (domain.Identity, error) {
	if token == "" {
		return domain.Identity{}, domain.ErrInvalidToken
	}
	hash := hashToken(token)
	sess, acc, err := s.repo.SessionByHash(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Identity{}, domain.ErrInvalidToken
	}
	if err != nil {
		return domain.Identity{}, err
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) {
		return domain.Identity{}, domain.ErrInvalidToken
	}
	if now.Sub(sess.LastSeenAt) > sessionRefresh {
		if err := s.repo.ExtendSession(ctx, hash, now, now.Add(sessionTTL)); err != nil {
			s.log.Warn("could not extend session", "err", err)
		}
	}
	return identityOf(acc, token), nil
}

// Profile returns the account data carried by the identity.
func (s *InternalAuth) Profile(_ context.Context, id domain.Identity) (domain.Profile, error) {
	return domain.Profile{Email: id.Email, EmailVerified: id.EmailVerified, Name: id.Name}, nil
}

// ResendVerification sends a new email verification link to the signed-in
// account, unless its email is already verified.
func (s *InternalAuth) ResendVerification(ctx context.Context, id domain.Identity) error {
	if id.EmailVerified {
		return nil
	}
	if err := s.allow(ctx, s.limits.Account, "verify:"+id.Email); err != nil {
		return err
	}
	acc, err := s.repo.AccountByEmail(ctx, id.Email)
	if err != nil {
		return err
	}
	s.sendVerification(ctx, acc)
	return nil
}

// VerifyEmail confirms the email with the token sent by email.
func (s *InternalAuth) VerifyEmail(ctx context.Context, token string) error {
	_, err := s.repo.VerifyEmail(ctx, hashToken(token), s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrInvalidAuthToken
	}
	return err
}

// RequestPasswordReset emails a reset link when the account exists. It
// answers the same either way, so it does not reveal who has an account.
func (s *InternalAuth) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.allow(ctx, s.limits.Account, "reset:"+email); err != nil {
		return err
	}
	acc, err := s.repo.AccountByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	expires := s.now().Add(resetPasswordTTL)
	if err := s.repo.CreateToken(ctx, domain.AuthToken{
		TokenHash: hash, AccountID: acc.ID, Purpose: domain.TokenResetPassword, ExpiresAt: expires,
	}); err != nil {
		return err
	}
	if s.mailer == nil {
		s.log.Warn("no mailer configured: password reset email not sent", "account_id", acc.ID)
		return nil
	}
	if err := s.mailer.SendPasswordReset(ctx, acc.Email, acc.Name, token, expires); err != nil {
		s.log.Error("sending password reset email", "account_id", acc.ID, "err", err)
	}
	return nil
}

// ResetPassword sets a new password with the token sent by email. Every open
// session ends, and the user signs in again.
func (s *InternalAuth) ResetPassword(ctx context.Context, token, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	_, err = s.repo.ResetPassword(ctx, hashToken(token), hash, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrInvalidAuthToken
	}
	return err
}

func (s *InternalAuth) newSession(ctx context.Context, acc domain.AuthAccount) (Session, error) {
	token, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	sess := domain.AuthSession{TokenHash: hash, AccountID: acc.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(sessionTTL)}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: sess.ExpiresAt}, nil
}

// sendVerification emails a verification link. A failure is logged and does
// not undo the sign-up: the user can ask for a new link.
func (s *InternalAuth) sendVerification(ctx context.Context, acc domain.AuthAccount) {
	token, hash, err := newToken()
	if err == nil {
		err = s.repo.CreateToken(ctx, domain.AuthToken{
			TokenHash: hash, AccountID: acc.ID, Purpose: domain.TokenVerifyEmail, ExpiresAt: s.now().Add(verifyEmailTTL),
		})
	}
	if err != nil {
		s.log.Error("creating email verification token", "account_id", acc.ID, "err", err)
		return
	}
	if s.mailer == nil {
		s.log.Warn("no mailer configured: email verification not sent", "account_id", acc.ID)
		return
	}
	if err := s.mailer.SendEmailVerification(ctx, acc.Email, acc.Name, token); err != nil {
		s.log.Error("sending email verification", "account_id", acc.ID, "err", err)
	}
}

func (s *InternalAuth) allow(ctx context.Context, l domain.RateLimiter, key string) error {
	if l == nil {
		return nil
	}
	ok, err := l.Allow(ctx, key)
	if err != nil {
		// A rate limiter outage must not lock everyone out.
		s.log.Warn("rate limiter unavailable", "err", err)
		return nil
	}
	if !ok {
		return domain.ErrTooManyAttempts
	}
	return nil
}

func identityOf(acc domain.AuthAccount, token string) domain.Identity {
	return domain.Identity{
		Provider:      domain.AuthProviderInternal,
		Subject:       acc.ID.String(),
		Email:         acc.Email,
		EmailVerified: acc.EmailVerifiedAt != nil,
		Name:          acc.Name,
		Token:         token,
	}
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > maxEmailLength {
		return "", domain.ErrInvalidEmail
	}
	return email, nil
}

func checkPassword(p string) error {
	n := utf8.RuneCountInString(p)
	if n < minPasswordLength || n > maxPasswordLength {
		return domain.ErrWeakPassword
	}
	return nil
}

// newToken returns a random token for the client and the hash to store.
func newToken() (string, []byte, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generating token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
