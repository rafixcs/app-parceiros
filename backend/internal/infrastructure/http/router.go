// Package http is the HTTP delivery layer: the base router, the middlewares
// and the handlers that turn requests into service calls. It is the only
// layer that knows status codes and the pt-BR messages shown to customers.
package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// Checker checks an external dependency (Postgres, Redis...).
type Checker func(ctx context.Context) error

// NewRouter creates the router with the health endpoints. The handlers
// register their own routes under /v1.
func NewRouter(log *slog.Logger, checks map[string]Checker) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(log))

	r.Get("/healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		httputil.JSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", readyHandler(checks))
	return r
}

func readyHandler(checks map[string]Checker) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status := nethttp.StatusOK
		result := make(map[string]string, len(checks))
		for name, check := range checks {
			if err := check(ctx); err != nil {
				status = nethttp.StatusServiceUnavailable
				result[name] = "failed"
				continue
			}
			result[name] = "ok"
		}
		httputil.JSON(w, status, result)
	}
}

func requestLogger(log *slog.Logger) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			// Log the route pattern, not the path, so tokens carried in the
			// URL (e.g. /v1/invites/{token}) never reach the logs.
			route := ""
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				route = rctx.RoutePattern()
			}
			if route == "" {
				route = "unknown"
			}
			log.Info("http",
				"method", r.Method,
				"route", route,
				"status", ww.Status(),
				"dur_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
