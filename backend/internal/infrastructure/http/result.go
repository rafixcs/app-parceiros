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

// ResultService is what the results handler needs (service.ResultService).
type ResultService interface {
	Mine(ctx context.Context, m domain.Member, from, to string) (domain.MyResults, error)
	Group(ctx context.Context, m domain.Member, from, to string) (domain.GroupResults, error)
	SetConsent(ctx context.Context, m domain.Member, shares bool) error
	SyncStatus(ctx context.Context, userID uuid.UUID) (domain.ConversionSync, error)
	RequestSync(ctx context.Context, userID uuid.UUID) (domain.ConversionSync, error)
}

// ResultHandler serves the dashboards under /v1/workspaces/{workspaceId} and
// the sync, which belongs to the user, under /v1/me.
type ResultHandler struct {
	svc ResultService
	log *slog.Logger
}

func NewResultHandler(svc ResultService, log *slog.Logger) *ResultHandler {
	return &ResultHandler{svc: svc, log: log}
}

const maxResultBody = 1 << 10

func (h *ResultHandler) Routes() Routes {
	return Routes{
		User: func(r chi.Router) {
			r.Get("/v1/me/results/sync", h.syncStatus)
			r.Post("/v1/me/results/sync", h.requestSync)
		},
		Workspace: func(r chi.Router) {
			r.Get("/results", h.mine)
			r.Get("/results/group", h.group)
		},
		// Giving or withdrawing consent works even with the workspace
		// suspended.
		WorkspaceAnyStatus: func(r chi.Router) {
			r.Put("/results/consent", h.consent)
		},
	}
}

func (h *ResultHandler) mine(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	out, err := h.svc.Mine(r.Context(), currentMember(r), q.Get("from"), q.Get("to"))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, myResultsResponseOf(out))
}

func (h *ResultHandler) group(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	out, err := h.svc.Group(r.Context(), currentMember(r), q.Get("from"), q.Get("to"))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, groupResultsResponseOf(out))
}

func (h *ResultHandler) consent(w nethttp.ResponseWriter, r *nethttp.Request) {
	var in consentRequest
	if !decodeBody(w, r, maxResultBody, &in) {
		return
	}
	if in.SharesResults == nil {
		httputil.Error(w, nethttp.StatusBadRequest, CodeInvalidJSON, Message(CodeInvalidJSON))
		return
	}
	if err := h.svc.SetConsent(r.Context(), currentMember(r), *in.SharesResults); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, consentResponse{SharesResults: *in.SharesResults})
}

func (h *ResultHandler) syncStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	s, err := h.svc.SyncStatus(r.Context(), currentUser(r).ID)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, syncResponseOf(s))
}

func (h *ResultHandler) requestSync(w nethttp.ResponseWriter, r *nethttp.Request) {
	s, err := h.svc.RequestSync(r.Context(), currentUser(r).ID)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusAccepted, syncResponseOf(s))
}
