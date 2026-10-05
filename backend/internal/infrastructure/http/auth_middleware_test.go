package http_test

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
)

func TestRequireIdentity(t *testing.T) {
	h := httpapi.RequireIdentity(auth.Dev{})(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		id, _ := httpapi.IdentityFromContext(r.Context())
		_, _ = w.Write([]byte(id.Subject))
	}))
	for header, want := range map[string]int{
		"":               nethttp.StatusUnauthorized,
		"Basic abc":      nethttp.StatusUnauthorized,
		"Bearer invalid": nethttp.StatusUnauthorized,
		"Bearer dev:ana": nethttp.StatusOK,
	} {
		req := httptest.NewRequest(nethttp.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Authorization %q: status %d, want %d", header, rec.Code, want)
		}
		if want == nethttp.StatusOK && rec.Body.String() != "ana" {
			t.Errorf("identity %q", rec.Body.String())
		}
	}
}
