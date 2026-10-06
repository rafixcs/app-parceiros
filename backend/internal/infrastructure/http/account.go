package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// AccountService is what the account handler needs from the use cases.
type AccountService interface {
	SignIn(ctx context.Context, id domain.Identity) (domain.User, error)
	Member(ctx context.Context, userID, workspaceID uuid.UUID) (domain.Member, error)
	Workspaces(ctx context.Context, userID uuid.UUID) ([]domain.WorkspaceView, error)
	CreateMentorship(ctx context.Context, userID uuid.UUID, name string, photo *string) (domain.WorkspaceView, error)
	Workspace(ctx context.Context, m domain.Member) (domain.WorkspaceView, error)
	UpdateWorkspace(ctx context.Context, m domain.Member, name, photo *string) (domain.WorkspaceView, error)
	Members(ctx context.Context, m domain.Member) ([]domain.MemberDetail, error)
	RemoveMember(ctx context.Context, m domain.Member, target uuid.UUID) error
	Invites(ctx context.Context, m domain.Member) ([]domain.Invite, error)
	CreateInvite(ctx context.Context, m domain.Member, email *string, validity time.Duration) (domain.CreatedInvite, error)
	RevokeInvite(ctx context.Context, m domain.Member, id uuid.UUID) error
	ViewInvite(ctx context.Context, token string) (domain.PublicInvite, error)
	AcceptInvite(ctx context.Context, userID uuid.UUID, token string) (domain.WorkspaceView, error)
}

type (
	userKey   struct{}
	memberKey struct{}
)

// UserFromContext returns the signed-in user (routes under Authenticated).
func UserFromContext(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userKey{}).(domain.User)
	return u, ok
}

// MemberFromContext returns the membership in the workspace of the route
// (routes under RequireMember).
func MemberFromContext(ctx context.Context) (domain.Member, bool) {
	m, ok := ctx.Value(memberKey{}).(domain.Member)
	return m, ok
}

// WithMember returns ctx carrying the membership, for tests.
func WithMember(ctx context.Context, m domain.Member) context.Context {
	return context.WithValue(ctx, memberKey{}, m)
}

func currentUser(r *nethttp.Request) domain.User {
	u, _ := UserFromContext(r.Context())
	return u
}

func currentMember(r *nethttp.Request) domain.Member {
	m, _ := MemberFromContext(r.Context())
	return m
}

// Routes are the routes of a feature, grouped by what they require.
type Routes struct {
	// Public routes need no sign-in.
	Public func(r chi.Router)
	// User routes need a signed-in user.
	User func(r chi.Router)
	// Workspace routes live under /v1/workspaces/{workspaceId}, for members
	// of a workspace with access.
	Workspace func(r chi.Router)
	// WorkspaceAnyStatus routes also live under the workspace, and keep
	// working while it is suspended (the subscription, for instance).
	WorkspaceAnyStatus func(r chi.Router)
}

// AccountHandler serves users, workspaces, members and invites, and mounts
// the routes of the other features behind its middlewares.
type AccountHandler struct {
	svc AccountService
	log *slog.Logger
}

func NewAccountHandler(svc AccountService, log *slog.Logger) *AccountHandler {
	return &AccountHandler{svc: svc, log: log}
}

const maxAccountBody = 64 << 10

// Authenticated resolves the user of the identity (creating them on first
// access) and keeps it in the context. It runs after RequireIdentity.
func (h *AccountHandler) Authenticated(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok {
			httputil.Error(w, nethttp.StatusUnauthorized, CodeUnauthenticated, Message(CodeUnauthenticated))
			return
		}
		u, err := h.svc.SignIn(r.Context(), id)
		if err != nil {
			WriteError(w, r, h.log, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

// RequireMember checks that the user belongs to the workspace {workspaceId}
// of the route and keeps the membership in the context. Whoever does not
// belong gets 404.
func (h *AccountHandler) RequireMember(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		wsID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
		if err != nil {
			WriteError(w, r, h.log, domain.ErrWorkspaceNotFound)
			return
		}
		m, err := h.svc.Member(r.Context(), currentUser(r).ID, wsID)
		if err != nil {
			WriteError(w, r, h.log, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithMember(r.Context(), m)))
	})
}

// RequireActive blocks the routes of a workspace suspended for lack of
// payment (402). It runs after RequireMember.
func (h *AccountHandler) RequireActive(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if m := currentMember(r); m.Suspended {
			if m.Role == domain.RoleOwner {
				WriteError(w, r, h.log, domain.ErrWorkspaceSuspendedOwner)
			} else {
				WriteError(w, r, h.log, domain.ErrWorkspaceSuspended)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Mount registers the account routes, and those of the features, in r. The
// authenticated routes use a.
func (h *AccountHandler) Mount(r chi.Router, a domain.Authenticator, features ...Routes) {
	r.Get("/v1/invites/{token}", h.viewInvite)
	for _, f := range features {
		if f.Public != nil {
			f.Public(r)
		}
	}

	r.Group(func(r chi.Router) {
		r.Use(RequireIdentity(a), h.Authenticated)

		r.Get("/v1/me", h.me)
		r.Get("/v1/workspaces", h.listWorkspaces)
		r.Post("/v1/workspaces", h.createMentorship)
		r.Post("/v1/invites/{token}/accept", h.acceptInvite)
		for _, f := range features {
			if f.User != nil {
				f.User(r)
			}
		}

		r.Route("/v1/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(h.RequireMember)
			// A suspended workspace can still be seen and left, and its
			// affiliates removed (to fit in fewer seats).
			r.Get("/", h.getWorkspace)
			r.Delete("/members/{userId}", h.removeMember)
			for _, f := range features {
				if f.WorkspaceAnyStatus != nil {
					f.WorkspaceAnyStatus(r)
				}
			}
			r.Group(func(r chi.Router) {
				r.Use(h.RequireActive)
				r.Patch("/", h.updateWorkspace)
				r.Get("/members", h.listMembers)
				r.Get("/invites", h.listInvites)
				r.Post("/invites", h.createInvite)
				r.Delete("/invites/{inviteId}", h.revokeInvite)
				for _, f := range features {
					if f.Workspace != nil {
						f.Workspace(r)
					}
				}
			})
		})
	})
}

func (h *AccountHandler) me(w nethttp.ResponseWriter, r *nethttp.Request) {
	httputil.JSON(w, nethttp.StatusOK, userResponseOf(currentUser(r)))
}

func (h *AccountHandler) listWorkspaces(w nethttp.ResponseWriter, r *nethttp.Request) {
	ws, err := h.svc.Workspaces(r.Context(), currentUser(r).ID)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(ws, workspaceResponseOf))
}

func (h *AccountHandler) createMentorship(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req createMentorshipRequest
	if !decodeBody(w, r, maxAccountBody, &req) {
		return
	}
	ws, err := h.svc.CreateMentorship(r.Context(), currentUser(r).ID, req.Name, req.PhotoURL)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusCreated, workspaceResponseOf(ws))
}

func (h *AccountHandler) getWorkspace(w nethttp.ResponseWriter, r *nethttp.Request) {
	ws, err := h.svc.Workspace(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, workspaceResponseOf(ws))
}

func (h *AccountHandler) updateWorkspace(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req updateWorkspaceRequest
	if !decodeBody(w, r, maxAccountBody, &req) {
		return
	}
	ws, err := h.svc.UpdateWorkspace(r.Context(), currentMember(r), req.Name, req.PhotoURL)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, workspaceResponseOf(ws))
}

func (h *AccountHandler) listMembers(w nethttp.ResponseWriter, r *nethttp.Request) {
	ms, err := h.svc.Members(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(ms, memberResponseOf))
}

func (h *AccountHandler) removeMember(w nethttp.ResponseWriter, r *nethttp.Request) {
	target, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrMemberNotFound)
		return
	}
	if err := h.svc.RemoveMember(r.Context(), currentMember(r), target); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AccountHandler) listInvites(w nethttp.ResponseWriter, r *nethttp.Request) {
	is, err := h.svc.Invites(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(is, inviteResponseOf))
}

func (h *AccountHandler) createInvite(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req createInviteRequest
	if !decodeBody(w, r, maxAccountBody, &req) {
		return
	}
	c, err := h.svc.CreateInvite(r.Context(), currentMember(r), req.Email, time.Duration(req.ValidityHours)*time.Hour)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	out := inviteResponseOf(c.Invite)
	out.Token, out.URL, out.EmailSent = c.Token, c.URL, c.EmailSent
	httputil.JSON(w, nethttp.StatusCreated, out)
}

func (h *AccountHandler) revokeInvite(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "inviteId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrInviteNotFound)
		return
	}
	if err := h.svc.RevokeInvite(r.Context(), currentMember(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AccountHandler) viewInvite(w nethttp.ResponseWriter, r *nethttp.Request) {
	inv, err := h.svc.ViewInvite(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, publicInviteResponse{
		WorkspaceName: inv.WorkspaceName, WorkspacePhotoURL: inv.WorkspacePhotoURL,
		Status: string(inv.Status), ExpiresAt: inv.ExpiresAt,
	})
}

func (h *AccountHandler) acceptInvite(w nethttp.ResponseWriter, r *nethttp.Request) {
	ws, err := h.svc.AcceptInvite(r.Context(), currentUser(r).ID, chi.URLParam(r, "token"))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, workspaceResponseOf(ws))
}

// mapSlice converts every element of a slice, returning an empty (not nil)
// slice so the JSON is [] rather than null.
func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
