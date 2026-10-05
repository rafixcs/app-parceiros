package http

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type identityKey struct{}

// RequireIdentity demands a valid Bearer token, checked by the configured
// identity provider, and stores the identity in the request context.
func RequireIdentity(a domain.Authenticator) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			token := BearerToken(r)
			if token == "" {
				unauthenticated(w)
				return
			}
			id, err := a.Authenticate(r.Context(), token)
			if err != nil {
				unauthenticated(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// BearerToken returns the token of the Authorization header, or "".
func BearerToken(r *nethttp.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return token
}

// IdentityFromContext returns the identity stored by RequireIdentity.
func IdentityFromContext(ctx context.Context) (domain.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(domain.Identity)
	return id, ok
}

// WithIdentity stores an identity in the context. Also used by tests.
func WithIdentity(ctx context.Context, id domain.Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

func unauthenticated(w nethttp.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer`)
	httputil.Error(w, nethttp.StatusUnauthorized, CodeUnauthenticated, Message(CodeUnauthenticated))
}
