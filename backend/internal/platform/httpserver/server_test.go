package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	ok := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("fora do ar") }

	tests := []struct {
		name   string
		checks map[string]Checker
		want   int
		body   map[string]string
	}{
		{"tudo ok", map[string]Checker{"postgres": ok, "redis": ok}, http.StatusOK,
			map[string]string{"postgres": "ok", "redis": "ok"}},
		{"redis fora", map[string]Checker{"postgres": ok, "redis": fail}, http.StatusServiceUnavailable,
			map[string]string{"postgres": "ok", "redis": "falhou"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), tt.checks)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, quer %d", rec.Code, tt.want)
			}
			var got map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			for k, v := range tt.body {
				if got[k] != v {
					t.Errorf("%s = %q, quer %q", k, got[k], v)
				}
			}
		})
	}
}
