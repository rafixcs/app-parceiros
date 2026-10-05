package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

type mailbox struct {
	mu     sync.Mutex
	verify map[string]string
	reset  map[string]string
}

func (m *mailbox) SendEmailVerification(_ context.Context, to, _, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify[to] = token
	return nil
}

func (m *mailbox) SendPasswordReset(_ context.Context, to, _, token string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset[to] = token
	return nil
}

// TestInternalAuthEndToEnd signs up, signs in and out through the HTTP API
// with AUTH_PROVIDER=internal, and checks that the accounts module sees the
// same user behind every session.
func TestInternalAuthEndToEnd(t *testing.T) {
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	box := &mailbox{verify: map[string]string{}, reset: map[string]string{}}
	svc, err := service.NewInternalAuth(repository.NewPostgresAuth(pool), auth.Argon2id{Memory: 1024, Time: 1, Threads: 1},
		box, service.AuthLimits{}, log)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	identity := identityProvider{Authenticator: svc, internal: svc}
	router := newRouter(log, pool, rdb, identity, contas.NewService(pool, identity, "https://app.test"), nil)

	call := func(token, method, path string, body, out any) int {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, r)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if out != nil {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
			}
		}
		return rec.Code
	}
	type session struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	type user struct {
		ID            string `json:"id"`
		Name          string `json:"nome"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verificado"`
	}
	type apiError struct {
		Code    string `json:"codigo"`
		Message string `json:"mensagem"`
	}

	var s1 session
	if code := call("", "POST", "/v1/auth/register", map[string]string{"name": "Ana", "email": "ana@example.com", "password": "secret-123"}, &s1); code != http.StatusCreated || s1.Token == "" {
		t.Fatalf("register: %d %+v", code, s1)
	}
	var e apiError
	if code := call("", "POST", "/v1/auth/register", map[string]string{"name": "Ana", "email": "ana@example.com", "password": "secret-123"}, &e); code != http.StatusConflict || e.Code != "email_taken" || e.Message == "" {
		t.Fatalf("duplicate register: %d %+v", code, e)
	}

	var me user
	if code := call(s1.Token, "GET", "/v1/eu", nil, &me); code != http.StatusOK || me.Email != "ana@example.com" || me.Name != "Ana" || me.EmailVerified {
		t.Fatalf("me after register: %d %+v", code, me)
	}
	var workspaces []map[string]any
	if code := call(s1.Token, "GET", "/v1/workspaces", nil, &workspaces); code != http.StatusOK || len(workspaces) != 1 {
		t.Fatalf("personal workspace: %d %v", code, workspaces)
	}

	// Verifying the email reaches the accounts module on the next request.
	if code := call("", "POST", "/v1/auth/email/verify", map[string]string{"token": box.verify["ana@example.com"]}, nil); code != http.StatusNoContent {
		t.Fatalf("verify: %d", code)
	}
	var again user
	if call(s1.Token, "GET", "/v1/eu", nil, &again); !again.EmailVerified || again.ID != me.ID {
		t.Fatalf("me after verify: %+v (was %+v)", again, me)
	}

	if code := call("", "POST", "/v1/auth/login", map[string]string{"email": "ana@example.com", "password": "nope-nope"}, &e); code != http.StatusUnauthorized || e.Code != "invalid_credentials" {
		t.Fatalf("bad login: %d %+v", code, e)
	}
	var s2 session
	if code := call("", "POST", "/v1/auth/login", map[string]string{"email": "ana@example.com", "password": "secret-123"}, &s2); code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	var same user
	if call(s2.Token, "GET", "/v1/eu", nil, &same); same.ID != me.ID {
		t.Fatalf("second session is another user: %+v", same)
	}
	if code := call(s2.Token, "POST", "/v1/auth/logout", nil, nil); code != http.StatusNoContent {
		t.Fatalf("logout: %d", code)
	}
	if code := call(s2.Token, "GET", "/v1/eu", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", code)
	}

	// Password reset ends the remaining session.
	if code := call("", "POST", "/v1/auth/password/forgot", map[string]string{"email": "ana@example.com"}, nil); code != http.StatusAccepted {
		t.Fatalf("forgot: %d", code)
	}
	if code := call("", "POST", "/v1/auth/password/reset", map[string]string{"token": box.reset["ana@example.com"], "password": "new-secret-1"}, nil); code != http.StatusNoContent {
		t.Fatalf("reset: %d", code)
	}
	if code := call(s1.Token, "GET", "/v1/eu", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("old session after reset: %d", code)
	}
	if code := call("", "POST", "/v1/auth/password/reset", map[string]string{"token": box.reset["ana@example.com"], "password": "new-secret-2"}, &e); code != http.StatusGone || e.Code != "invalid_auth_token" {
		t.Fatalf("reset token reused: %d %+v", code, e)
	}
	if code := call("", "POST", "/v1/auth/login", map[string]string{"email": "ana@example.com", "password": "new-secret-1", "extra": "x"}, &e); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
}
