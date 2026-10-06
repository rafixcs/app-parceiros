package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Role of a member in a workspace.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleMentor    Role = "mentor"
	RoleAffiliate Role = "affiliate"
)

// Manages says whether the role manages the group (invites, lists and removes
// members, curates lists). The owner of a mentorship is also a mentor.
func (r Role) Manages() bool { return r == RoleOwner || r == RoleMentor }

// WorkspaceKind tells a user's personal workspace from a mentorship.
type WorkspaceKind string

const (
	WorkspacePersonal   WorkspaceKind = "personal"
	WorkspaceMentorship WorkspaceKind = "mentorship"
)

// Plans and the keys of their limits and prices (table plan_limits).
const (
	PlanSolo       = "solo"
	PlanMentorship = "mentorship"

	LimitSeats          = "seats"
	LimitTrialSeats     = "trial_seats"
	LimitLists          = "lists"
	LimitVideoBytes     = "video_bytes"
	LimitPriceCents     = "price_cents"
	LimitSeatPriceCents = "seat_price_cents"
)

// AccessStatus of a workspace in its subscription. Trial, active and free give
// access; suspended does not.
type AccessStatus string

const (
	AccessTrial     AccessStatus = "trial"
	AccessActive    AccessStatus = "active"
	AccessSuspended AccessStatus = "suspended"
	// AccessFree is the personal workspace of a student (affiliate) of a
	// mentorship that is up to date: by Rafael's provisional decision
	// (2026-10-05) it is not charged. The rule may change to the student paying
	// a seat; see docs/mvp.md §8.
	AccessFree AccessStatus = "free"
)

// User is a person signed in through one identity provider.
type User struct {
	ID            uuid.UUID
	Name          string
	Email         string
	EmailVerified bool
	CreatedAt     time.Time
}

// NewUser is the data of a user on first access, or of a profile change.
type NewUser struct {
	AuthProvider  string
	AuthSubject   string
	Name          string
	Email         string
	EmailVerified bool
}

// Workspace is where data is shared: the user's personal one, or a
// mentorship with its group of affiliates.
type Workspace struct {
	ID       uuid.UUID
	Kind     WorkspaceKind
	Name     string
	PhotoURL *string
	OwnerID  uuid.UUID
	Plan     string
	// AccessUntil is how long the workspace can be used: the end of the trial
	// or of the paid cycle, plus the grace period.
	AccessUntil time.Time
	// PaidAt is the last confirmed payment; nil during the trial.
	PaidAt *time.Time
	// Seats bought by a mentorship; nil before the first payment.
	Seats     *int32
	CreatedAt time.Time
}

// AccessStatus computes the status at now. student says whether the owner is
// an affiliate of a mentorship that is up to date, which frees their personal
// workspace.
func (w Workspace) AccessStatus(now time.Time, student bool) AccessStatus {
	current := now.Before(w.AccessUntil)
	switch {
	case current && w.PaidAt != nil:
		return AccessActive
	case student && w.Kind == WorkspacePersonal:
		return AccessFree
	case !current:
		return AccessSuspended
	default:
		return AccessTrial
	}
}

// NewWorkspace is the data of a workspace to create, with its owner.
type NewWorkspace struct {
	Kind     WorkspaceKind
	Name     string
	PhotoURL *string
	OwnerID  uuid.UUID
	Plan     string
}

// WorkspaceUpdate changes name and photo. Nil fields stay; ClearPhoto removes
// the photo.
type WorkspaceUpdate struct {
	Name       *string
	PhotoURL   *string
	ClearPhoto bool
}

// Membership is a workspace seen by one of its members.
type Membership struct {
	Workspace     Workspace
	Role          Role
	SharesResults bool
}

// WorkspaceView is a workspace as the member sees it: with their role and the
// access status.
type WorkspaceView struct {
	Workspace
	Role   Role
	Status AccessStatus
}

// Member is the signed-in user's membership in the workspace of a request.
type Member struct {
	WorkspaceID   uuid.UUID
	UserID        uuid.UUID
	Role          Role
	WorkspaceKind WorkspaceKind
	// SharesResults says whether the user lets owner and mentors see their
	// aggregated results in this workspace (LGPD consent).
	SharesResults bool
	// Suspended says whether the workspace has no access for lack of payment.
	Suspended bool
}

// Actor is who a repository call acts for. Repositories turn it into the
// tenant isolation of the store (row level security on Postgres).
type Actor struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
}

// Actor returns the member acting in their workspace.
func (m Member) Actor() Actor { return Actor{UserID: m.UserID, WorkspaceID: m.WorkspaceID} }

// MemberDetail is a member as the group's managers see them.
type MemberDetail struct {
	UserID   uuid.UUID
	Name     string
	Email    string
	Role     Role
	JoinedAt time.Time
	// SharesResults says whether the member lets the group dashboard show
	// their aggregated results.
	SharesResults bool
}

// Contact is where a user's notifications go.
type Contact struct {
	Name          string
	Email         string
	EmailVerified bool
}

// InviteStatus of an invite link.
type InviteStatus string

const (
	InviteValid   InviteStatus = "valid"
	InviteExpired InviteStatus = "expired"
	InviteUsed    InviteStatus = "used"
	InviteRevoked InviteStatus = "revoked"
)

// Invite is a single-use link that makes the user who accepts it an affiliate
// of a mentorship. Only the hash of its token is stored.
type Invite struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	// Email restricts the invite to the user signed in with that (verified)
	// address.
	Email     *string
	ExpiresAt time.Time
	UsedBy    *uuid.UUID
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Status of the invite at now.
func (i Invite) Status(now time.Time) InviteStatus {
	switch {
	case i.RevokedAt != nil:
		return InviteRevoked
	case i.UsedBy != nil:
		return InviteUsed
	case !now.Before(i.ExpiresAt):
		return InviteExpired
	default:
		return InviteValid
	}
}

// NewInvite is the data of an invite to create.
type NewInvite struct {
	Email     *string
	TokenHash []byte
	ExpiresAt time.Time
	CreatedBy uuid.UUID
}

// CreatedInvite is an invite just created: the only moment its token exists.
type CreatedInvite struct {
	Invite
	Token string
	URL   string
	// EmailSent is set for an invite with email when invite emails are on.
	EmailSent *bool
}

// PublicInvite is what whoever has the link sees before accepting.
type PublicInvite struct {
	WorkspaceName     string
	WorkspacePhotoURL *string
	Status            InviteStatus
	ExpiresAt         time.Time
}

// InviteEmail is what the email of an invite needs.
type InviteEmail struct {
	Email         string
	WorkspaceName string
	URL           string
	ExpiresAt     time.Time
}

// InviteMailer sends the invite email.
type InviteMailer interface {
	SendInvite(ctx context.Context, e InviteEmail) error
}

// Transactor runs a function in one transaction. Repository calls made with
// the context it passes join that transaction.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// AccountRepository stores users, workspaces, members, invites and plan
// limits. Missing records return ErrNotFound.
type AccountRepository interface {
	UserByIdentity(ctx context.Context, provider, subject string) (User, error)
	// SaveUser creates the user of an identity or updates their profile, and
	// says whether it was created.
	SaveUser(ctx context.Context, u NewUser) (User, bool, error)
	UserByID(ctx context.Context, a Actor, id uuid.UUID) (User, error)

	// CreateWorkspace creates the workspace with its owner as a member.
	CreateWorkspace(ctx context.Context, w NewWorkspace) (Workspace, error)
	UserWorkspaces(ctx context.Context, userID uuid.UUID) ([]Membership, error)
	// IsStudent says whether the user is an affiliate of a mentorship that is
	// up to date.
	IsStudent(ctx context.Context, userID uuid.UUID) (bool, error)
	// Membership returns the user's membership in the workspace, or
	// ErrNotFound when they are not a member.
	Membership(ctx context.Context, userID, workspaceID uuid.UUID) (Membership, error)
	Workspace(ctx context.Context, a Actor) (Workspace, error)
	// LockWorkspace reads the workspace locking it until the end of the
	// transaction, to serialize the seat count.
	LockWorkspace(ctx context.Context, a Actor) (Workspace, error)
	UpdateWorkspace(ctx context.Context, a Actor, u WorkspaceUpdate) (Workspace, error)
	// GrantAccess extends the access until `until` (never shortens it) and
	// records a payment; non-nil seats replace the ones bought.
	GrantAccess(ctx context.Context, workspaceID uuid.UUID, until time.Time, seats *int32) error
	// RevokeAccess suspends the workspace now.
	RevokeAccess(ctx context.Context, workspaceID uuid.UUID) error
	SetSeats(ctx context.Context, a Actor, seats int32) error
	PlanLimit(ctx context.Context, plan, key string) (int64, error)

	Members(ctx context.Context, a Actor) ([]MemberDetail, error)
	MemberRole(ctx context.Context, a Actor, userID uuid.UUID) (Role, error)
	AddMember(ctx context.Context, a Actor, userID uuid.UUID, role Role) error
	RemoveMember(ctx context.Context, a Actor, userID uuid.UUID) error
	SetSharesResults(ctx context.Context, a Actor, shares bool) error
	CountAffiliates(ctx context.Context, a Actor) (int64, error)

	CreateInvite(ctx context.Context, a Actor, i NewInvite) (Invite, error)
	PendingInvites(ctx context.Context, a Actor) ([]Invite, error)
	CountPendingInvites(ctx context.Context, a Actor) (int64, error)
	RevokeInvite(ctx context.Context, a Actor, id uuid.UUID) error
	// InviteByHash finds the invite of a token. With lock, the invite stays
	// locked until the end of the transaction.
	InviteByHash(ctx context.Context, a Actor, hash []byte, lock bool) (Invite, error)
	MarkInviteUsed(ctx context.Context, a Actor, inviteID, userID uuid.UUID) error
}

// Account errors.
var (
	ErrWorkspaceNotFound       = NewError(KindNotFound, "workspace_not_found")
	ErrMemberNotFound          = NewError(KindNotFound, "member_not_found")
	ErrInviteNotFound          = NewError(KindNotFound, "invite_not_found")
	ErrForbidden               = NewError(KindForbidden, "forbidden")
	ErrMentorshipOnly          = NewError(KindConflict, "mentorship_only")
	ErrConsentMentorshipOnly   = NewError(KindConflict, "consent_mentorship_only")
	ErrOwnerCannotLeave        = NewError(KindConflict, "owner_cannot_leave")
	ErrNoSeats                 = NewError(KindConflict, "no_seats")
	ErrSeatsInUse              = NewError(KindConflict, "seats_in_use")
	ErrSeatsAbovePlan          = NewError(KindInvalid, "seats_above_plan")
	ErrWorkspaceSuspended      = NewError(KindPaymentRequired, "workspace_suspended")
	ErrWorkspaceSuspendedOwner = NewError(KindPaymentRequired, "workspace_suspended_owner")
	ErrAlreadyMember           = NewError(KindConflict, "already_member")
	ErrInviteExpired           = NewError(KindGone, "invite_expired")
	ErrInviteUsed              = NewError(KindGone, "invite_used")
	ErrInviteRevoked           = NewError(KindGone, "invite_revoked")
	ErrInviteOtherEmail        = NewError(KindForbidden, "invite_other_email")
	ErrInvalidWorkspaceName    = NewError(KindInvalid, "invalid_workspace_name")
	ErrInvalidPhotoURL         = NewError(KindInvalid, "invalid_photo_url")
	ErrInvalidInviteEmail      = NewError(KindInvalid, "invalid_invite_email")
	ErrInvalidInviteValidity   = NewError(KindInvalid, "invalid_invite_validity")
)
