package http

import (
	"context"
	"io"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// BillingService is what the billing handler needs from the use cases.
type BillingService interface {
	View(ctx context.Context, m domain.Member) (domain.SubscriptionView, error)
	Subscribe(ctx context.Context, m domain.Member, n service.NewSubscriptionRequest) (domain.SubscriptionView, error)
	ChangeSeats(ctx context.Context, m domain.Member, seats int64) (domain.SubscriptionView, error)
	Cancel(ctx context.Context, m domain.Member) (domain.SubscriptionView, error)
	SimulatePayment(ctx context.Context, m domain.Member) (domain.SubscriptionView, error)
	Webhook(ctx context.Context, h domain.HeaderReader, body []byte) error
}

// BillingHandler serves the workspace subscription and the gateway webhook.
type BillingHandler struct {
	svc BillingService
	log *slog.Logger
}

func NewBillingHandler(svc BillingService, log *slog.Logger) *BillingHandler {
	return &BillingHandler{svc: svc, log: log}
}

const (
	maxBillingBody = 4 << 10
	maxWebhookBody = 1 << 20
)

// Routes puts the subscription under the workspace outside the suspension
// for lack of payment (it is how the workspace comes back), and the gateway
// webhook without sign-in: the gateway proves itself with its secret.
func (h *BillingHandler) Routes() Routes {
	return Routes{
		Public: func(r chi.Router) {
			r.Post("/v1/webhooks/billing", h.webhook)
		},
		WorkspaceAnyStatus: func(r chi.Router) {
			r.Get("/subscription", h.view)
			r.Post("/subscription", h.subscribe)
			r.Patch("/subscription", h.changeSeats)
			r.Delete("/subscription", h.cancel)
			r.Post("/subscription/simulate-payment", h.simulatePayment)
		},
	}
}

func (h *BillingHandler) respond(w nethttp.ResponseWriter, r *nethttp.Request, status int, v domain.SubscriptionView, err error) {
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, status, subscriptionResponseOf(v))
}

func (h *BillingHandler) view(w nethttp.ResponseWriter, r *nethttp.Request) {
	v, err := h.svc.View(r.Context(), currentMember(r))
	h.respond(w, r, nethttp.StatusOK, v, err)
}

func (h *BillingHandler) subscribe(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req subscribeRequest
	if !decodeBody(w, r, maxBillingBody, &req) {
		return
	}
	v, err := h.svc.Subscribe(r.Context(), currentMember(r), service.NewSubscriptionRequest{Seats: req.Seats, TaxID: req.TaxID})
	h.respond(w, r, nethttp.StatusCreated, v, err)
}

func (h *BillingHandler) changeSeats(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req changeSeatsRequest
	if !decodeBody(w, r, maxBillingBody, &req) {
		return
	}
	v, err := h.svc.ChangeSeats(r.Context(), currentMember(r), req.Seats)
	h.respond(w, r, nethttp.StatusOK, v, err)
}

func (h *BillingHandler) cancel(w nethttp.ResponseWriter, r *nethttp.Request) {
	v, err := h.svc.Cancel(r.Context(), currentMember(r))
	h.respond(w, r, nethttp.StatusOK, v, err)
}

func (h *BillingHandler) simulatePayment(w nethttp.ResponseWriter, r *nethttp.Request) {
	v, err := h.svc.SimulatePayment(r.Context(), currentMember(r))
	h.respond(w, r, nethttp.StatusOK, v, err)
}

// webhook receives a gateway event. The answer is short and has no details:
// whoever calls here is not signed in. Events processed, ignored or of an
// unknown subscription answer 204, so the gateway does not redeliver them.
func (h *BillingHandler) webhook(w nethttp.ResponseWriter, r *nethttp.Request) {
	body, err := io.ReadAll(nethttp.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrInvalidBillingWebhook)
		return
	}
	if err := h.svc.Webhook(r.Context(), r.Header, body); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}
