package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, "/healthz", nil))
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	ok := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("down") }

	tests := []struct {
		name   string
		checks map[string]Checker
		want   int
		body   map[string]string
	}{
		{"all ok", map[string]Checker{"postgres": ok, "redis": ok}, nethttp.StatusOK,
			map[string]string{"postgres": "ok", "redis": "ok"}},
		{"redis down", map[string]Checker{"postgres": ok, "redis": fail}, nethttp.StatusServiceUnavailable,
			map[string]string{"postgres": "ok", "redis": "failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), tt.checks)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, "/readyz", nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			var got map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			for k, v := range tt.body {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
