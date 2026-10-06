package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type userResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

func userResponseOf(u domain.User) userResponse {
	return userResponse{ID: u.ID, Name: u.Name, Email: u.Email, EmailVerified: u.EmailVerified, CreatedAt: u.CreatedAt}
}

type workspaceResponse struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	PhotoURL *string   `json:"photo_url"`
	Plan     string    `json:"plan"`
	Status   string    `json:"status"`
	// AccessUntil is how long the workspace can be used: the end of the trial
	// or of the paid cycle, plus the grace period.
	AccessUntil time.Time `json:"access_until"`
	// Seats bought (mentorship). Null before the first payment.
	Seats     *int32    `json:"seats"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func workspaceResponseOf(w domain.WorkspaceView) workspaceResponse {
	return workspaceResponse{
		ID: w.ID, Kind: string(w.Kind), Name: w.Name, PhotoURL: w.PhotoURL, Plan: w.Plan,
		Status: string(w.Status), AccessUntil: w.AccessUntil, Seats: w.Seats, Role: string(w.Role),
		CreatedAt: w.CreatedAt,
	}
}

type createMentorshipRequest struct {
	Name     string  `json:"name"`
	PhotoURL *string `json:"photo_url"`
}

type updateWorkspaceRequest struct {
	Name     *string `json:"name"`
	PhotoURL *string `json:"photo_url"`
}

type memberResponse struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
	// SharesResults says whether the member lets the group dashboard show
	// their aggregated results.
	SharesResults bool `json:"shares_results"`
}

func memberResponseOf(m domain.MemberDetail) memberResponse {
	return memberResponse{
		UserID: m.UserID, Name: m.Name, Email: m.Email, Role: string(m.Role),
		JoinedAt: m.JoinedAt, SharesResults: m.SharesResults,
	}
}

type createInviteRequest struct {
	Email         *string `json:"email"`
	ValidityHours int     `json:"validity_hours"`
}

type inviteResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     *string   `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	// Token and URL only show in the creation response; the database keeps
	// the hash.
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
	// EmailSent only shows when creating an invite with email.
	EmailSent *bool `json:"email_sent,omitempty"`
}

func inviteResponseOf(i domain.Invite) inviteResponse {
	return inviteResponse{ID: i.ID, Email: i.Email, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
}

// publicInviteResponse is what whoever has the link sees before accepting.
type publicInviteResponse struct {
	WorkspaceName     string    `json:"workspace_name"`
	WorkspacePhotoURL *string   `json:"workspace_photo_url"`
	Status            string    `json:"status"`
	ExpiresAt         time.Time `json:"expires_at"`
}
