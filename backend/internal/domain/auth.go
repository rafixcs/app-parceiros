package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Identity providers. The active one is chosen by configuration
// (AUTH_PROVIDER); a user belongs to the provider that created it.
const (
	AuthProviderOIDC     = "oidc"
	AuthProviderInternal = "internal"
	AuthProviderDev      = "dev"
)

// Identity is who made the request, according to the identity provider.
// Email and Name may be empty: not every access token carries those claims.
type Identity struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	// Token is the raw access token, kept to fetch the profile from the
	// provider. It must never be logged or returned.
	Token string
}

// Profile is the user's data at the provider, used on first access.
type Profile struct {
	Email         string
	EmailVerified bool
	Name          string
}

// Authenticator validates access tokens and fetches the user's profile. Every
// identity provider (external OIDC, internal email and password, dev)
// implements it, and the rest of the application depends only on it.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Identity, error)
	// Profile returns email and name, using the identity's claims when
	// present and asking the provider otherwise.
	Profile(ctx context.Context, id Identity) (Profile, error)
}

// ErrInvalidToken means the token is missing, malformed, expired or revoked.
var ErrInvalidToken = errors.New("invalid token")

// AuthAccount is an account of the internal identity provider. Its ID is the
// subject of the identities it issues.
type AuthAccount struct {
	ID              uuid.UUID
	Email           string
	Name            string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

// AuthSession is a signed-in session of the internal provider. Only the hash
// of the token is stored.
type AuthSession struct {
	TokenHash  []byte
	AccountID  uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// AuthTokenPurpose says what a one-time token of the internal provider is for.
type AuthTokenPurpose string

const (
	TokenVerifyEmail   AuthTokenPurpose = "verify_email"
	TokenResetPassword AuthTokenPurpose = "reset_password"
)

// AuthToken is a one-time token sent by email (email verification and
// password reset). Only its hash is stored.
type AuthToken struct {
	TokenHash []byte
	AccountID uuid.UUID
	Purpose   AuthTokenPurpose
	ExpiresAt time.Time
}

// AuthRepository stores the accounts, sessions and one-time tokens of the
// internal identity provider.
type AuthRepository interface {
	// CreateAccount fails with ErrEmailTaken when the email is in use.
	CreateAccount(ctx context.Context, a AuthAccount) (AuthAccount, error)
	AccountByEmail(ctx context.Context, email string) (AuthAccount, error)
	CreateSession(ctx context.Context, s AuthSession) error
	// SessionByHash returns the session and its account.
	SessionByHash(ctx context.Context, hash []byte) (AuthSession, AuthAccount, error)
	ExtendSession(ctx context.Context, hash []byte, lastSeen, expires time.Time) error
	DeleteSession(ctx context.Context, hash []byte) error
	CreateToken(ctx context.Context, t AuthToken) error
	// VerifyEmail consumes a valid email verification token and marks the
	// email of its account as verified, atomically.
	VerifyEmail(ctx context.Context, tokenHash []byte, now time.Time) (AuthAccount, error)
	// ResetPassword consumes a valid password reset token, replaces the
	// password and ends every session of the account, atomically. The reset
	// also proves the email, so it is marked as verified.
	ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string, now time.Time) (AuthAccount, error)
}

// PasswordHasher hashes and checks passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) (bool, error)
}

// AuthMailer sends the emails of the internal provider. Messages are written
// in the customer's language by the implementation.
type AuthMailer interface {
	SendEmailVerification(ctx context.Context, to, name, token string) error
	SendPasswordReset(ctx context.Context, to, name, token string, expiresAt time.Time) error
}

// RateLimiter answers whether one more attempt is allowed for a key.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// Business errors of the internal provider.
var (
	ErrInvalidCredentials = NewError(KindUnauthenticated, "invalid_credentials")
	ErrEmailTaken         = NewError(KindConflict, "email_taken")
	ErrInvalidEmail       = NewError(KindInvalid, "invalid_email")
	ErrInvalidName        = NewError(KindInvalid, "invalid_name")
	ErrWeakPassword       = NewError(KindInvalid, "weak_password")
	ErrInvalidAuthToken   = NewError(KindGone, "invalid_auth_token")
	ErrTooManyAttempts    = NewError(KindTooManyRequests, "too_many_attempts")
)
