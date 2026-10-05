package http

import (
	"errors"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

var statusByKind = map[domain.ErrorKind]int{
	domain.KindInvalid:         nethttp.StatusUnprocessableEntity,
	domain.KindUnauthenticated: nethttp.StatusUnauthorized,
	domain.KindForbidden:       nethttp.StatusForbidden,
	domain.KindNotFound:        nethttp.StatusNotFound,
	domain.KindConflict:        nethttp.StatusConflict,
	domain.KindGone:            nethttp.StatusGone,
	domain.KindPaymentRequired: nethttp.StatusPaymentRequired,
	domain.KindTooManyRequests: nethttp.StatusTooManyRequests,
}

// WriteError answers a service error. A domain.Error becomes its status and
// pt-BR message; anything else is logged and answered as a 500 that reveals
// nothing.
func WriteError(w nethttp.ResponseWriter, r *nethttp.Request, log *slog.Logger, err error) {
	var de *domain.Error
	if errors.As(err, &de) {
		status, ok := statusByKind[de.Kind]
		if !ok {
			status = nethttp.StatusInternalServerError
		}
		httputil.Error(w, status, de.Code, Message(de.Code))
		return
	}
	log.Error("request failed", "err", err, "request_id", middleware.GetReqID(r.Context()))
	httputil.Error(w, nethttp.StatusInternalServerError, CodeInternal, Message(CodeInternal))
}
