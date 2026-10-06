package http

import (
	"context"
	"log/slog"
	"net"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// InternalAuthService is what the auth handler needs from the internal
// identity provider.
type InternalAuthService interface {
	domain.Authenticator
	Register(ctx context.Context, in service.RegisterInput) (service.Session, error)
	Login(ctx context.Context, email, password, clientIP string) (service.Session, error)
	Logout(ctx context.Context, token string) error
	ResendVerification(ctx context.Context, id domain.Identity) error
	VerifyEmail(ctx context.Context, token string) error
	RequestPasswordReset(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, password string) error
}

// AuthHandler serves the routes of the internal identity provider. They are
// registered only when AUTH_PROVIDER=internal.
type AuthHandler struct {
	svc InternalAuthService
	log *slog.Logger
}

func NewAuthHandler(svc InternalAuthService, log *slog.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, log: log}
}

const maxAuthBody = 4 << 10

// Routes registers the /v1/auth routes in r.
func (h *AuthHandler) Routes(r chi.Router) {
	r.Route("/v1/auth", func(r chi.Router) {
		r.Post("/register", h.register)
		r.Post("/login", h.login)
		r.Post("/email/verify", h.verifyEmail)
		r.Post("/password/forgot", h.forgotPassword)
		r.Post("/password/reset", h.resetPassword)
		r.Group(func(r chi.Router) {
			r.Use(RequireIdentity(h.svc))
			r.Post("/logout", h.logout)
			r.Post("/email/resend", h.resendVerification)
		})
	})
}

func (h *AuthHandler) register(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req registerRequest
	if !h.decode(w, r, &req) {
		return
	}
	s, err := h.svc.Register(r.Context(), service.RegisterInput{Name: req.Name, Email: req.Email, Password: req.Password})
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusCreated, sessionResponse{Token: s.Token, ExpiresAt: s.ExpiresAt.UTC().Truncate(time.Second)})
}

func (h *AuthHandler) login(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req loginRequest
	if !h.decode(w, r, &req) {
		return
	}
	s, err := h.svc.Login(r.Context(), req.Email, req.Password, clientIP(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, sessionResponse{Token: s.Token, ExpiresAt: s.ExpiresAt.UTC().Truncate(time.Second)})
}

func (h *AuthHandler) logout(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.svc.Logout(r.Context(), BearerToken(r)); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AuthHandler) resendVerification(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, _ := IdentityFromContext(r.Context())
	if err := h.svc.ResendVerification(r.Context(), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AuthHandler) verifyEmail(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req tokenRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.VerifyEmail(r.Context(), req.Token); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AuthHandler) forgotPassword(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req forgotPasswordRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), req.Email); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusAccepted)
}

func (h *AuthHandler) resetPassword(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req resetPasswordRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *AuthHandler) decode(w nethttp.ResponseWriter, r *nethttp.Request, v any) bool {
	return decodeBody(w, r, maxAuthBody, v)
}

// clientIP returns the address of the client: the first X-Forwarded-For hop
// set by the load balancer, or the peer address. It only feeds rate limits.
func clientIP(r *nethttp.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
