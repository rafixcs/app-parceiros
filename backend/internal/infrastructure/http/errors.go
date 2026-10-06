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
	domain.KindUnavailable:     nethttp.StatusServiceUnavailable,
	domain.KindUpstream:        nethttp.StatusBadGateway,
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

// decodeBody reads the JSON request body into v. On failure it answers 400
// and returns false.
func decodeBody(w nethttp.ResponseWriter, r *nethttp.Request, maxBytes int64, v any) bool {
	if err := httputil.DecodeJSON(w, r, maxBytes, v); err != nil {
		httputil.Error(w, nethttp.StatusBadRequest, CodeInvalidJSON, Message(CodeInvalidJSON))
		return false
	}
	return true
}

// invalidRequest answers 400 for a malformed path or query parameter.
func invalidRequest(w nethttp.ResponseWriter) {
	httputil.Error(w, nethttp.StatusBadRequest, CodeInvalidRequest, Message(CodeInvalidRequest))
}
