package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// testApp is the whole API on a real Postgres, with the dev identity
// provider (Bearer dev:<sub>) and fakes for the external services.
type testApp struct {
	t      *testing.T
	pool   *pgxpool.Pool
	svcs   *services
	router http.Handler
	// notices holds the enqueued deliver_notification jobs (see
	// deliverNotifications).
	notices *fakeNotificationQueue
}

func newTestApp(t *testing.T, opts ...func(*infra)) *testApp {
	t.Helper()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	identity := identityProvider{Authenticator: auth.Dev{}}
	notices := &fakeNotificationQueue{}
	in := infra{log: log, pool: pool, appURL: "https://app.test", profiles: identity, notificationQueue: notices}
	// The Shopee mock and a vault by default; options may replace them, or
	// clear the catalog to run without the app credential.
	in.shopee = shopee.NewMock(&shopee.Mock{}, shopee.Config{})
	in.catalog = shopee.AppCatalog{Client: in.shopee, Credential: shopee.Credential{AppID: "1", Secret: "mock"}}
	in.secrets = newTestVault(t)
	for _, o := range opts {
		o(&in)
	}
	svcs := newServices(in)
	return &testApp{t: t, pool: pool, svcs: svcs, router: newRouter(in.log, nil, identity, svcs), notices: notices}
}

// newTestVault is a vault with a random master key.
func newTestVault(t *testing.T) *crypto.Vault {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	kek, err := crypto.NewLocalKEK("test-1", base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return crypto.NewVault(kek)
}

// call makes a request as the user `sub` (empty: signed out) and decodes the
// answer into out, when not nil.
func (a *testApp) call(sub, method, path string, body, out any) int {
	a.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if sub != "" {
		req.Header.Set("Authorization", "Bearer dev:"+sub)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: invalid answer %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec.Code
}

// must makes the request and fails the test unless it answers status.
func (a *testApp) must(sub, method, path string, body, out any, status int) {
	a.t.Helper()
	var e httputil.ErrorBody
	if out == nil {
		out = &e
	}
	if got := a.call(sub, method, path, body, out); got != status {
		a.t.Fatalf("%s %s as %q: status %d, want %d (%+v)", method, path, sub, got, status, out)
	}
}

// mustFail makes the request and fails the test unless it answers status
// with the error code.
func (a *testApp) mustFail(sub, method, path string, body any, status int, code string) {
	a.t.Helper()
	var e httputil.ErrorBody
	got := a.call(sub, method, path, body, &e)
	if got != status || e.Code != code {
		a.t.Fatalf("%s %s as %q: %d %q, want %d %q", method, path, sub, got, e.Code, status, code)
	}
	if e.Message == "" {
		a.t.Fatalf("%s %s: code %q came without a message", method, path, code)
	}
}

// admin runs SQL as the owner of the tables, to set up scenarios (e.g. to
// expire an invite). It does not go through the API role.
func (a *testApp) admin(sql string, args ...any) {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		a.t.Fatal(err)
	}
}

// JSON of the account routes, as clients see it.

type userJSON struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
}

type workspaceJSON struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	PhotoURL    *string   `json:"photo_url"`
	Plan        string    `json:"plan"`
	Status      string    `json:"status"`
	AccessUntil time.Time `json:"access_until"`
	Seats       *int32    `json:"seats"`
	Role        string    `json:"role"`
}

type memberJSON struct {
	UserID        uuid.UUID `json:"user_id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	SharesResults bool      `json:"shares_results"`
}

type inviteJSON struct {
	ID        uuid.UUID `json:"id"`
	Email     *string   `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	EmailSent *bool     `json:"email_sent"`
}

type publicInviteJSON struct {
	WorkspaceName string `json:"workspace_name"`
	Status        string `json:"status"`
}

func (a *testApp) me(sub string) userJSON {
	a.t.Helper()
	var u userJSON
	a.must(sub, http.MethodGet, "/v1/me", nil, &u, http.StatusOK)
	return u
}

func (a *testApp) workspaces(sub string) []workspaceJSON {
	a.t.Helper()
	var ws []workspaceJSON
	a.must(sub, http.MethodGet, "/v1/workspaces", nil, &ws, http.StatusOK)
	return ws
}

// personal returns the user's personal workspace.
func (a *testApp) personal(sub string) workspaceJSON {
	a.t.Helper()
	for _, w := range a.workspaces(sub) {
		if w.Kind == "personal" {
			return w
		}
	}
	a.t.Fatalf("%s has no personal workspace", sub)
	return workspaceJSON{}
}

func (a *testApp) mentorship(sub, name string) workspaceJSON {
	a.t.Helper()
	var ws workspaceJSON
	a.must(sub, http.MethodPost, "/v1/workspaces", map[string]any{"name": name}, &ws, http.StatusCreated)
	return ws
}

func (a *testApp) invite(sub string, ws uuid.UUID, body map[string]any) inviteJSON {
	a.t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	var i inviteJSON
	a.must(sub, http.MethodPost, wsPath(ws, "/invites"), body, &i, http.StatusCreated)
	return i
}

// join makes sub an affiliate of the mentorship ws, invited by its owner.
func (a *testApp) join(owner, sub string, ws uuid.UUID) userJSON {
	a.t.Helper()
	i := a.invite(owner, ws, nil)
	a.must(sub, http.MethodPost, "/v1/invites/"+i.Token+"/accept", nil, nil, http.StatusOK)
	return a.me(sub)
}

func wsPath(ws uuid.UUID, path string) string {
	return "/v1/workspaces/" + ws.String() + path
}
