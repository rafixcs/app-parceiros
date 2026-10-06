package http

import (
	"context"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// NotificationService is what the notification handler needs from the use
// cases.
type NotificationService interface {
	Inbox(ctx context.Context, m domain.Member) (domain.Inbox, error)
	MarkRead(ctx context.Context, m domain.Member, id uuid.UUID) error
	MarkAllRead(ctx context.Context, m domain.Member) error
	Preferences(ctx context.Context, userID uuid.UUID) (domain.NotificationPreferences, error)
	SetEmail(ctx context.Context, userID uuid.UUID, email *bool) (domain.NotificationPreferences, error)
	Subscribe(ctx context.Context, userID uuid.UUID, sub domain.PushSubscription) error
	Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error
}

// NotificationHandler serves the inbox, the preferences and the Web Push
// subscriptions.
type NotificationHandler struct {
	svc NotificationService
	log *slog.Logger
}

func NewNotificationHandler(svc NotificationService, log *slog.Logger) *NotificationHandler {
	return &NotificationHandler{svc: svc, log: log}
}

const maxNotificationBody = 8 << 10

// Routes registers the preferences and push under /v1/me and the inbox under
// /v1/workspaces/{workspaceId}.
func (h *NotificationHandler) Routes() Routes {
	return Routes{
		User: func(r chi.Router) {
			r.Get("/v1/me/notifications", h.preferences)
			r.Put("/v1/me/notifications", h.setPreferences)
			r.Post("/v1/me/push", h.subscribe)
			r.Delete("/v1/me/push", h.unsubscribe)
		},
		Workspace: func(r chi.Router) {
			r.Get("/notifications", h.inbox)
			r.Post("/notifications/read", h.markAllRead)
			r.Post("/notifications/{notificationId}/read", h.markRead)
		},
	}
}

func (h *NotificationHandler) preferences(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := h.svc.Preferences(r.Context(), currentUser(r).ID)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, notificationPreferencesResponseOf(p))
}

func (h *NotificationHandler) setPreferences(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req setNotificationPreferencesRequest
	if !decodeBody(w, r, maxNotificationBody, &req) {
		return
	}
	p, err := h.svc.SetEmail(r.Context(), currentUser(r).ID, req.Email)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, notificationPreferencesResponseOf(p))
}

func (h *NotificationHandler) subscribe(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req pushSubscriptionRequest
	if !decodeBody(w, r, maxNotificationBody, &req) {
		return
	}
	sub := domain.PushSubscription{Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth}
	if err := h.svc.Subscribe(r.Context(), currentUser(r).ID, sub); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *NotificationHandler) unsubscribe(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req pushUnsubscribeRequest
	if !decodeBody(w, r, maxNotificationBody, &req) {
		return
	}
	if err := h.svc.Unsubscribe(r.Context(), currentUser(r).ID, req.Endpoint); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *NotificationHandler) inbox(w nethttp.ResponseWriter, r *nethttp.Request) {
	in, err := h.svc.Inbox(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, inboxResponse{
		Notifications: mapSlice(in.Notifications, notificationResponseOf), Unread: in.Unread,
	})
}

func (h *NotificationHandler) markAllRead(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.svc.MarkAllRead(r.Context(), currentMember(r)); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *NotificationHandler) markRead(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "notificationId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrNotificationNotFound)
		return
	}
	if err := h.svc.MarkRead(r.Context(), currentMember(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}
