package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	personalWorkspaceName = "Pessoal"
	// DefaultInviteValidity and MaxInviteValidity bound how long an invite
	// link works.
	DefaultInviteValidity = 7 * 24 * time.Hour
	MaxInviteValidity     = 30 * 24 * time.Hour
	maxWorkspaceName      = 80
	maxPhotoURL           = 2048
	inviteTokenBytes      = 32
)

// ProfileSource fetches the profile of an identity at its provider (the
// domain.Authenticator in use).
type ProfileSource interface {
	Profile(ctx context.Context, id domain.Identity) (domain.Profile, error)
}

// AccountService handles users, workspaces, members (roles) and invites.
type AccountService struct {
	repo     domain.AccountRepository
	tx       domain.Transactor
	profiles ProfileSource
	appURL   string
	// invites emails the invite links. Nil sends nothing: the API only
	// returns the link.
	invites domain.InviteMailer
	now     func() time.Time
}

// NewAccountService builds the service. appURL is the base of the invite links
// (e.g. https://app.example.com.br). The worker, which only reads contacts,
// passes a nil profiles.
func NewAccountService(repo domain.AccountRepository, tx domain.Transactor, profiles ProfileSource, appURL string) *AccountService {
	return &AccountService{
		repo: repo, tx: tx, profiles: profiles, appURL: strings.TrimRight(appURL, "/"), now: time.Now,
	}
}

// SendInvitesWith turns on the invite emails.
func (s *AccountService) SendInvitesWith(m domain.InviteMailer) { s.invites = m }

// SignIn returns the user of the identity. On first access it creates the
// user and their personal workspace. Later it updates name and email when the
// identity brings new data (for instance, the email verified at the internal
// provider).
func (s *AccountService) SignIn(ctx context.Context, id domain.Identity) (domain.User, error) {
	u, err := s.repo.UserByIdentity(ctx, id.Provider, id.Subject)
	if err == nil && !profileChanged(u, id) {
		return u, nil
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, err
	}

	var profile domain.Profile
	if err == nil {
		profile = domain.Profile{Email: id.Email, EmailVerified: id.EmailVerified, Name: id.Name}
	} else if profile, err = s.profiles.Profile(ctx, id); err != nil {
		return domain.User{}, fmt.Errorf("fetching profile from the identity provider: %w", err)
	}
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name, _, _ = strings.Cut(profile.Email, "@")
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var created bool
		u, created, err = s.repo.SaveUser(ctx, domain.NewUser{
			AuthProvider:  id.Provider,
			AuthSubject:   id.Subject,
			Name:          name,
			Email:         strings.ToLower(strings.TrimSpace(profile.Email)),
			EmailVerified: profile.EmailVerified,
		})
		if err != nil || !created {
			return err
		}
		_, err = s.repo.CreateWorkspace(ctx, domain.NewWorkspace{
			Kind: domain.WorkspacePersonal, Name: personalWorkspaceName, OwnerID: u.ID, Plan: domain.PlanSolo,
		})
		return err
	})
	if err != nil {
		return domain.User{}, err
	}
	return u, nil
}

// profileChanged says whether the identity carries a name or email different
// from the stored ones. Tokens without those claims (common with OIDC) change
// nothing.
func profileChanged(u domain.User, id domain.Identity) bool {
	if id.Email == "" || id.Name == "" {
		return false
	}
	return !strings.EqualFold(u.Email, id.Email) || u.EmailVerified != id.EmailVerified || u.Name != strings.TrimSpace(id.Name)
}

// Contact returns name and email of a user, for the notifications.
func (s *AccountService) Contact(ctx context.Context, userID uuid.UUID) (domain.Contact, error) {
	u, err := s.repo.UserByID(ctx, domain.Actor{UserID: userID}, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Contact{}, domain.ErrMemberNotFound
	}
	return domain.Contact{Name: u.Name, Email: u.Email, EmailVerified: u.EmailVerified}, err
}

// Workspaces lists the workspaces the user belongs to (the top switcher).
func (s *AccountService) Workspaces(ctx context.Context, userID uuid.UUID) ([]domain.WorkspaceView, error) {
	ms, err := s.repo.UserWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	student, err := s.repo.IsStudent(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.WorkspaceView, 0, len(ms))
	for _, m := range ms {
		out = append(out, s.view(m.Workspace, m.Role, student))
	}
	return out, nil
}

func (s *AccountService) view(w domain.Workspace, r domain.Role, student bool) domain.WorkspaceView {
	return domain.WorkspaceView{Workspace: w, Role: r, Status: w.AccessStatus(s.now(), student)}
}

// CreateMentorship creates a mentorship workspace owned by the user.
func (s *AccountService) CreateMentorship(ctx context.Context, userID uuid.UUID, name string, photo *string) (domain.WorkspaceView, error) {
	name, err := checkWorkspaceName(name)
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	if photo, err = checkPhotoURL(photo); err != nil {
		return domain.WorkspaceView{}, err
	}
	w, err := s.repo.CreateWorkspace(ctx, domain.NewWorkspace{
		Kind: domain.WorkspaceMentorship, Name: name, PhotoURL: photo, OwnerID: userID, Plan: domain.PlanMentorship,
	})
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	return s.view(w, domain.RoleOwner, false), nil
}

// isStudent says whether the workspace is the personal one of a student of a
// mentorship that is up to date (and so not charged).
func (s *AccountService) isStudent(ctx context.Context, w domain.Workspace, userID uuid.UUID) (bool, error) {
	if w.Kind != domain.WorkspacePersonal {
		return false, nil
	}
	return s.repo.IsStudent(ctx, userID)
}

// Member returns the user's membership in the workspace, or
// ErrWorkspaceNotFound when they are not a member (without revealing whether
// the workspace exists).
func (s *AccountService) Member(ctx context.Context, userID, workspaceID uuid.UUID) (domain.Member, error) {
	ms, err := s.repo.Membership(ctx, userID, workspaceID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Member{}, domain.ErrWorkspaceNotFound
	}
	if err != nil {
		return domain.Member{}, err
	}
	student, err := s.isStudent(ctx, ms.Workspace, userID)
	if err != nil {
		return domain.Member{}, err
	}
	return domain.Member{
		WorkspaceID: workspaceID, UserID: userID, Role: ms.Role,
		WorkspaceKind: ms.Workspace.Kind, SharesResults: ms.SharesResults,
		Suspended: ms.Workspace.AccessStatus(s.now(), student) == domain.AccessSuspended,
	}, nil
}

// Workspace returns the member's workspace.
func (s *AccountService) Workspace(ctx context.Context, m domain.Member) (domain.WorkspaceView, error) {
	w, err := s.repo.Workspace(ctx, m.Actor())
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	student, err := s.isStudent(ctx, w, m.UserID)
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	return s.view(w, m.Role, student), nil
}

// UpdateWorkspace changes name and photo. Only the owner can. An empty photo
// removes it.
func (s *AccountService) UpdateWorkspace(ctx context.Context, m domain.Member, name, photo *string) (domain.WorkspaceView, error) {
	if m.Role != domain.RoleOwner {
		return domain.WorkspaceView{}, domain.ErrForbidden
	}
	var u domain.WorkspaceUpdate
	if name != nil {
		n, err := checkWorkspaceName(*name)
		if err != nil {
			return domain.WorkspaceView{}, err
		}
		u.Name = &n
	}
	if photo != nil {
		if strings.TrimSpace(*photo) == "" {
			u.ClearPhoto = true
		} else {
			p, err := checkPhotoURL(photo)
			if err != nil {
				return domain.WorkspaceView{}, err
			}
			u.PhotoURL = p
		}
	}
	w, err := s.repo.UpdateWorkspace(ctx, m.Actor(), u)
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	student, err := s.isStudent(ctx, w, m.UserID)
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	return s.view(w, m.Role, student), nil
}

// Members lists the group. Only owner and mentor see it.
func (s *AccountService) Members(ctx context.Context, m domain.Member) ([]domain.MemberDetail, error) {
	if !m.Role.Manages() {
		return nil, domain.ErrForbidden
	}
	return s.repo.Members(ctx, m.Actor())
}

// SetSharesResults records whether the user lets owner and mentors see their
// aggregated results in the workspace. It only exists in mentorships and
// applies from the next dashboard query.
func (s *AccountService) SetSharesResults(ctx context.Context, m domain.Member, shares bool) error {
	if m.WorkspaceKind != domain.WorkspaceMentorship {
		return domain.ErrConsentMentorshipOnly
	}
	err := s.repo.SetSharesResults(ctx, m.Actor(), shares)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrMemberNotFound
	}
	return err
}

// RemoveMember takes someone out of the workspace. Any member can leave
// (target = themself), except the owner. Owner and mentor remove affiliates;
// only the owner removes mentors. The affiliate's personal data (saved items,
// credential) is not deleted: it stays theirs.
func (s *AccountService) RemoveMember(ctx context.Context, m domain.Member, target uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		role, err := s.repo.MemberRole(ctx, m.Actor(), target)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case role == domain.RoleOwner:
			return domain.ErrOwnerCannotLeave
		case target == m.UserID:
		case m.Role == domain.RoleOwner:
		case m.Role == domain.RoleMentor && role == domain.RoleAffiliate:
		default:
			return domain.ErrForbidden
		}
		return s.repo.RemoveMember(ctx, m.Actor(), target)
	})
}

// CreateInvite makes a single-use invite for an affiliate. With an email, only
// whoever signs in with that (verified) email can accept it, and the link also
// goes by email when invite emails are on (SendInvitesWith).
func (s *AccountService) CreateInvite(ctx context.Context, m domain.Member, email *string, validity time.Duration) (domain.CreatedInvite, error) {
	if !m.Role.Manages() {
		return domain.CreatedInvite{}, domain.ErrForbidden
	}
	if m.WorkspaceKind != domain.WorkspaceMentorship {
		return domain.CreatedInvite{}, domain.ErrMentorshipOnly
	}
	if validity == 0 {
		validity = DefaultInviteValidity
	}
	if validity < time.Hour || validity > MaxInviteValidity {
		return domain.CreatedInvite{}, domain.ErrInvalidInviteValidity
	}
	if email != nil {
		e, err := checkInviteEmail(*email)
		if err != nil {
			return domain.CreatedInvite{}, err
		}
		email = e
	}

	token, hash, err := newInviteToken()
	if err != nil {
		return domain.CreatedInvite{}, err
	}

	var inv domain.Invite
	var w domain.Workspace
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		w, err = s.repo.LockWorkspace(ctx, m.Actor())
		if err != nil {
			return err
		}
		limit, err := s.seats(ctx, w)
		if err != nil {
			return err
		}
		inUse, err := s.seatsInUse(ctx, m.Actor())
		if err != nil {
			return err
		}
		if inUse >= limit {
			return domain.ErrNoSeats
		}
		inv, err = s.repo.CreateInvite(ctx, m.Actor(), domain.NewInvite{
			Email: email, TokenHash: hash, ExpiresAt: s.now().Add(validity), CreatedBy: m.UserID,
		})
		return err
	})
	if err != nil {
		return domain.CreatedInvite{}, err
	}
	out := domain.CreatedInvite{Invite: inv, Token: token, URL: s.appURL + "/convite/" + token}
	if email != nil && s.invites != nil {
		// The invite already works: if the email fails, the mentor still has
		// the link.
		sent := s.invites.SendInvite(ctx, domain.InviteEmail{
			Email: *email, WorkspaceName: w.Name, URL: out.URL, ExpiresAt: inv.ExpiresAt,
		}) == nil
		out.EmailSent = &sent
	}
	return out, nil
}

// PlanLimit returns a limit of the workspace's plan (e.g. domain.LimitLists).
// A limit missing from the plan is zero.
func (s *AccountService) PlanLimit(ctx context.Context, m domain.Member, key string) (int64, error) {
	w, err := s.repo.Workspace(ctx, m.Actor())
	if err != nil {
		return 0, err
	}
	return s.repo.PlanLimit(ctx, w.Plan, key)
}

// seats returns the seats of the workspace: the ones bought or, before the
// first payment, the trial limit of the plan.
func (s *AccountService) seats(ctx context.Context, w domain.Workspace) (int64, error) {
	if w.Seats != nil {
		return int64(*w.Seats), nil
	}
	return s.repo.PlanLimit(ctx, w.Plan, domain.LimitTrialSeats)
}

// seatsInUse counts the affiliates and the pending invites, which already
// hold a seat.
func (s *AccountService) seatsInUse(ctx context.Context, a domain.Actor) (int64, error) {
	affiliates, err := s.repo.CountAffiliates(ctx, a)
	if err != nil {
		return 0, err
	}
	pending, err := s.repo.CountPendingInvites(ctx, a)
	return affiliates + pending, err
}

// SeatsInUse counts the affiliates and pending invites of the workspace.
func (s *AccountService) SeatsInUse(ctx context.Context, m domain.Member) (int64, error) {
	return s.seatsInUse(ctx, m.Actor())
}

// SetSeats changes the seats bought by the mentorship (subscriptions). It
// does not let them go below the ones in use nor above the plan maximum. Only
// the owner can.
func (s *AccountService) SetSeats(ctx context.Context, m domain.Member, n int32) error {
	if m.Role != domain.RoleOwner {
		return domain.ErrForbidden
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		w, err := s.repo.LockWorkspace(ctx, m.Actor())
		if err != nil {
			return err
		}
		if err := s.checkSeats(ctx, m.Actor(), w, int64(n)); err != nil {
			return err
		}
		return s.repo.SetSeats(ctx, m.Actor(), n)
	})
}

// CheckSeats checks whether the mentorship can buy n seats: not fewer than the
// ones in use nor more than the plan maximum.
func (s *AccountService) CheckSeats(ctx context.Context, m domain.Member, n int64) error {
	w, err := s.repo.Workspace(ctx, m.Actor())
	if err != nil {
		return err
	}
	return s.checkSeats(ctx, m.Actor(), w, n)
}

func (s *AccountService) checkSeats(ctx context.Context, a domain.Actor, w domain.Workspace, n int64) error {
	most, err := s.repo.PlanLimit(ctx, w.Plan, domain.LimitSeats)
	if err != nil {
		return err
	}
	if n > most {
		return domain.ErrSeatsAbovePlan
	}
	inUse, err := s.seatsInUse(ctx, a)
	if err != nil {
		return err
	}
	if n < inUse {
		return domain.ErrSeatsInUse
	}
	return nil
}

// GrantAccess records a payment: it extends the workspace access until
// `until` (never shortens it) and, when given, sets the seats bought. Called
// by the billing webhook, which has no user; it is idempotent.
func (s *AccountService) GrantAccess(ctx context.Context, workspaceID uuid.UUID, until time.Time, seats *int32) error {
	err := s.repo.GrantAccess(ctx, workspaceID, until, seats)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrWorkspaceNotFound
	}
	return err
}

// RevokeAccess suspends the workspace now (a refunded or disputed payment).
// It is idempotent.
func (s *AccountService) RevokeAccess(ctx context.Context, workspaceID uuid.UUID) error {
	return s.repo.RevokeAccess(ctx, workspaceID)
}

// Invites lists the pending invites (without the token).
func (s *AccountService) Invites(ctx context.Context, m domain.Member) ([]domain.Invite, error) {
	if !m.Role.Manages() {
		return nil, domain.ErrForbidden
	}
	return s.repo.PendingInvites(ctx, m.Actor())
}

// RevokeInvite cancels a pending invite.
func (s *AccountService) RevokeInvite(ctx context.Context, m domain.Member, id uuid.UUID) error {
	if !m.Role.Manages() {
		return domain.ErrForbidden
	}
	err := s.repo.RevokeInvite(ctx, m.Actor(), id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrInviteNotFound
	}
	return err
}

// ViewInvite shows the workspace and the status of the invite to whoever has
// the link. It needs no sign-in.
func (s *AccountService) ViewInvite(ctx context.Context, token string) (domain.PublicInvite, error) {
	hash, ok := hashInviteToken(token)
	if !ok {
		return domain.PublicInvite{}, domain.ErrInviteNotFound
	}
	inv, err := s.repo.InviteByHash(ctx, domain.Actor{}, hash, false)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.PublicInvite{}, domain.ErrInviteNotFound
	}
	if err != nil {
		return domain.PublicInvite{}, err
	}
	// The workspace comes from the invite in hand.
	w, err := s.repo.Workspace(ctx, domain.Actor{WorkspaceID: inv.WorkspaceID})
	out := domain.PublicInvite{
		WorkspaceName: w.Name, WorkspacePhotoURL: w.PhotoURL, Status: inv.Status(s.now()), ExpiresAt: inv.ExpiresAt,
	}
	if errors.Is(err, domain.ErrNotFound) {
		return domain.PublicInvite{}, domain.ErrInviteNotFound
	}
	return out, err
}

// AcceptInvite makes the user an affiliate of the invite's workspace.
func (s *AccountService) AcceptInvite(ctx context.Context, userID uuid.UUID, token string) (domain.WorkspaceView, error) {
	hash, ok := hashInviteToken(token)
	if !ok {
		return domain.WorkspaceView{}, domain.ErrInviteNotFound
	}
	var w domain.Workspace
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		inv, err := s.repo.InviteByHash(ctx, domain.Actor{UserID: userID}, hash, false)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrInviteNotFound
		}
		if err != nil {
			return err
		}
		// From here on the transaction acts in the invite's workspace.
		a := domain.Actor{UserID: userID, WorkspaceID: inv.WorkspaceID}
		if inv, err = s.repo.InviteByHash(ctx, a, hash, true); err != nil {
			return err
		}
		switch inv.Status(s.now()) {
		case domain.InviteRevoked:
			return domain.ErrInviteRevoked
		case domain.InviteUsed:
			return domain.ErrInviteUsed
		case domain.InviteExpired:
			return domain.ErrInviteExpired
		}

		if _, err := s.repo.MemberRole(ctx, a, userID); err == nil {
			return domain.ErrAlreadyMember
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if inv.Email != nil {
			u, err := s.repo.UserByID(ctx, a, userID)
			if err != nil {
				return err
			}
			if !u.EmailVerified || !strings.EqualFold(u.Email, *inv.Email) {
				return domain.ErrInviteOtherEmail
			}
		}

		w, err = s.repo.LockWorkspace(ctx, a)
		if err != nil {
			return err
		}
		limit, err := s.seats(ctx, w)
		if err != nil {
			return err
		}
		affiliates, err := s.repo.CountAffiliates(ctx, a)
		if err != nil {
			return err
		}
		if affiliates >= limit {
			return domain.ErrNoSeats
		}
		if err := s.repo.AddMember(ctx, a, userID, domain.RoleAffiliate); err != nil {
			return err
		}
		return s.repo.MarkInviteUsed(ctx, a, inv.ID, userID)
	})
	if err != nil {
		return domain.WorkspaceView{}, err
	}
	return s.view(w, domain.RoleAffiliate, false), nil
}

func newInviteToken() (string, []byte, error) {
	b := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}

// hashInviteToken returns the hash of a token with a valid format.
func hashInviteToken(token string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != inviteTokenBytes {
		return nil, false
	}
	h := sha256.Sum256([]byte(token))
	return h[:], true
}

func checkWorkspaceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxWorkspaceName {
		return "", domain.ErrInvalidWorkspaceName
	}
	return name, nil
}

func checkPhotoURL(photo *string) (*string, error) {
	if photo == nil {
		return nil, nil
	}
	p := strings.TrimSpace(*photo)
	if p == "" {
		return nil, nil
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(p) > maxPhotoURL {
		return nil, domain.ErrInvalidPhotoURL
	}
	return &p, nil
}

func checkInviteEmail(email string) (*string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return nil, nil
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return nil, domain.ErrInvalidInviteEmail
	}
	return &e, nil
}
