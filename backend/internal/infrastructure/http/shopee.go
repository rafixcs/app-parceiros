package http

import (
	"context"
	"errors"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// ShopeeCredentialService is what the Shopee handler needs from the use
// cases.
type ShopeeCredentialService interface {
	View(ctx context.Context, userID uuid.UUID) (domain.ShopeeConnection, error)
	Connect(ctx context.Context, userID uuid.UUID, appID, secret string) (domain.ShopeeConnection, error)
	Disconnect(ctx context.Context, userID uuid.UUID) error
}

// ShopeeHandler serves the user's connection to Shopee at /v1/me/shopee.
type ShopeeHandler struct {
	svc ShopeeCredentialService
	log *slog.Logger
}

func NewShopeeHandler(svc ShopeeCredentialService, log *slog.Logger) *ShopeeHandler {
	return &ShopeeHandler{svc: svc, log: log}
}

const maxShopeeBody = 4 << 10

func (h *ShopeeHandler) Routes() Routes {
	return Routes{User: func(r chi.Router) {
		r.Get("/v1/me/shopee", h.view)
		r.Put("/v1/me/shopee", h.connect)
		r.Delete("/v1/me/shopee", h.disconnect)
	}}
}

func (h *ShopeeHandler) view(w nethttp.ResponseWriter, r *nethttp.Request) {
	c, err := h.svc.View(r.Context(), currentUser(r).ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, shopeeConnectionResponseOf(c))
}

func (h *ShopeeHandler) connect(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req connectShopeeRequest
	if !decodeBody(w, r, maxShopeeBody, &req) {
		return
	}
	c, err := h.svc.Connect(r.Context(), currentUser(r).ID, req.AppID, req.Secret)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, shopeeConnectionResponseOf(c))
}

func (h *ShopeeHandler) disconnect(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.svc.Disconnect(r.Context(), currentUser(r).ID); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

// fail answers the error. When Shopee is unavailable, it logs the cause,
// which never holds the Secret (see domain.ShopeeCredential.String and
// shopee.Client).
func (h *ShopeeHandler) fail(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	if errors.Is(err, domain.ErrShopeeUnavailable) {
		h.log.WarnContext(r.Context(), "shopee unavailable while validating a credential", "err", err)
	}
	WriteError(w, r, h.log, err)
}
