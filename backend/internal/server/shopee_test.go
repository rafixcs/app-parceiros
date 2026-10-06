package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
)

const shopeeSecret = "s3cr3t-0f-sh0p33"

type shopeeConnectionJSON struct {
	Status     string     `json:"status"`
	AppID      *string    `json:"app_id"`
	VerifiedAt *time.Time `json:"verified_at"`
}

// withShopeeMock makes the API talk to the mock m, and logs into logs.
func withShopeeMock(m *shopee.Mock, logs *bytes.Buffer) func(*infra) {
	return func(in *infra) {
		in.shopee = shopee.NewMock(m, shopee.Config{})
		in.log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
}

// rawCall makes the request and returns the status and the raw body.
func (a *testApp) rawCall(sub, method, path string, body string) (int, string) {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer dev:"+sub)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestShopeeConnection(t *testing.T) {
	logs := &bytes.Buffer{}
	mock := &shopee.Mock{Secrets: map[string]string{"18300001234": shopeeSecret}}
	a := newTestApp(t, withShopeeMock(mock, logs))
	ctx := context.Background()
	svc := a.svcs.shopeeCredentials

	// raw reads a bytea column as the owner of the tables (no RLS).
	raw := func(sql string) []byte {
		var b []byte
		if err := a.pool.QueryRow(ctx, sql).Scan(&b); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		return b
	}

	var c shopeeConnectionJSON
	a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
	if c.Status != "disconnected" || c.AppID != nil || c.VerifiedAt != nil {
		t.Fatalf("before connecting: %+v", c)
	}
	a.mustFail("", http.MethodGet, "/v1/me/shopee", nil, http.StatusUnauthorized, "unauthenticated")

	t.Run("invalid data", func(t *testing.T) {
		a.mustFail("ana", http.MethodPut, "/v1/me/shopee", map[string]string{"app_id": "abc", "secret": "x"},
			http.StatusUnprocessableEntity, "invalid_app_id")
		a.mustFail("ana", http.MethodPut, "/v1/me/shopee", map[string]string{"app_id": "123456789012345678901", "secret": "x"},
			http.StatusUnprocessableEntity, "invalid_app_id")
		a.mustFail("ana", http.MethodPut, "/v1/me/shopee", map[string]string{"app_id": "123", "secret": "  "},
			http.StatusUnprocessableEntity, "invalid_secret")
		a.mustFail("ana", http.MethodPut, "/v1/me/shopee", map[string]string{"app_id": "123", "secret": strings.Repeat("x", 257)},
			http.StatusUnprocessableEntity, "invalid_secret")
		if st, _ := a.rawCall("ana", http.MethodPut, "/v1/me/shopee", `{"app_id":`); st != http.StatusBadRequest {
			t.Fatalf("broken JSON: %d", st)
		}
	})

	t.Run("Shopee refuses", func(t *testing.T) {
		cases := map[string]struct {
			app, secret string
			status      int
			code        string
		}{
			"wrong secret":  {"18300001234", "other-" + shopeeSecret, 422, "invalid_shopee_credential"},
			"signature":     {shopee.MockAppIDInvalid, shopeeSecret, 422, "invalid_shopee_credential"},
			"access denied": {shopee.MockAppIDAccessDenied, shopeeSecret, 422, "shopee_access_denied"},
			"rate limit":    {shopee.MockAppIDRateLimited, shopeeSecret, 429, "shopee_rate_limited"},
			"unavailable":   {shopee.MockAppIDUnavailable, shopeeSecret, 502, "shopee_unavailable"},
		}
		for name, c := range cases {
			a.mustFail("ana", http.MethodPut, "/v1/me/shopee", map[string]string{"app_id": c.app, "secret": c.secret}, c.status, c.code)
			_, body := a.rawCall("ana", http.MethodPut, "/v1/me/shopee", `{"app_id":"`+c.app+`","secret":"`+c.secret+`"}`)
			if strings.Contains(body, shopeeSecret) {
				t.Errorf("%s: secret in the answer", name)
			}
		}
		var c shopeeConnectionJSON
		a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
		if c.Status != "disconnected" {
			t.Fatalf("a refused credential was saved: %+v", c)
		}
	})

	st, body := a.rawCall("ana", http.MethodPut, "/v1/me/shopee", `{"app_id":" 18300001234 ","secret":"`+shopeeSecret+`"}`)
	if st != http.StatusOK || strings.Contains(body, shopeeSecret) {
		t.Fatalf("connect: %d %s", st, body)
	}
	a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
	if c.Status != "connected" || c.AppID == nil || *c.AppID != "••••1234" || c.VerifiedAt == nil {
		t.Fatalf("connected: %+v", c)
	}

	t.Run("encrypted in the database", func(t *testing.T) {
		sealed := raw("SELECT encrypted_secret || encrypted_dek FROM shopee_credentials")
		if len(sealed) == 0 || bytes.Contains(sealed, []byte(shopeeSecret)) {
			t.Fatal("secret in clear in the database")
		}
	})

	ana := a.me("ana").ID
	bia := a.me("bia").ID

	t.Run("decrypted only for the owner", func(t *testing.T) {
		cred, err := svc.UserCredential(ctx, ana)
		if err != nil || cred.Secret != shopeeSecret || cred.AppID != "18300001234" {
			t.Fatalf("%v %v", cred, err)
		}
		if _, err := svc.UserCredential(ctx, bia); !errors.Is(err, domain.ErrNoCredential) {
			t.Fatalf("bia: %v", err)
		}
		users, err := svc.ConnectedUsers(ctx)
		if err != nil || !slices.Contains(users, ana) || slices.Contains(users, bia) {
			t.Fatalf("connected users: %v %v", users, err)
		}
	})

	t.Run("no leak between users", func(t *testing.T) {
		var c shopeeConnectionJSON
		a.must("bia", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
		if c.Status != "disconnected" {
			t.Fatalf("bia sees ana's connection: %+v", c)
		}
		// Even a query with no filter, in bia's scope, does not see ana's row.
		var n int
		err := database.InTx(ctx, a.pool, database.Scope{UserID: bia.String()}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM shopee_credentials").Scan(&n)
		})
		if err != nil || n != 0 {
			t.Fatalf("RLS: %d rows, %v", n, err)
		}
		// Nor can it write one for ana.
		err = database.InTx(ctx, a.pool, database.Scope{UserID: bia.String()}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE shopee_credentials SET status = 'invalid'")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		// Disconnecting as bia does not delete ana's.
		a.must("bia", http.MethodDelete, "/v1/me/shopee", nil, nil, http.StatusNoContent)
		if _, err := svc.UserCredential(ctx, ana); err != nil {
			t.Fatalf("ana's credential is gone: %v", err)
		}
	})

	t.Run("affiliator and report use the user's credential", func(t *testing.T) {
		ok, err := a.svcs.affiliator.Connected(ctx, ana)
		if err != nil || !ok {
			t.Fatalf("connected: %v %v", ok, err)
		}
		link, err := a.svcs.affiliator.GenerateLink(ctx, ana, "https://shopee.com.br/product/300000000/22000084811", []string{"instagram", "w1"})
		if err != nil || !strings.HasPrefix(link, "https://s.shopee.com.br/") {
			t.Fatalf("link: %q %v", link, err)
		}
		now := time.Now()
		if _, err := a.svcs.conversionReport.Conversions(ctx, ana, now.Add(-30*24*time.Hour), now); err != nil {
			t.Fatalf("report: %v", err)
		}
		if _, err := a.svcs.affiliator.GenerateLink(ctx, bia, "https://shopee.com.br/product/1/2", nil); !errors.Is(err, domain.ErrNoCredential) {
			t.Fatalf("bia without credential: %v", err)
		}
		if _, err := a.svcs.conversionReport.Conversions(ctx, bia, now.Add(-time.Hour), now); !errors.Is(err, domain.ErrNoCredential) {
			t.Fatalf("bia report: %v", err)
		}
	})

	t.Run("refusal marks the status", func(t *testing.T) {
		// The affiliate changed the Secret at Shopee: the next call is
		// refused and the connection becomes invalid.
		mock.Secrets["18300001234"] = "new-secret"
		_, err := a.svcs.affiliator.GenerateLink(ctx, ana, "https://shopee.com.br/product/1/2", nil)
		if !errors.Is(err, domain.ErrSourceInvalidCredential) {
			t.Fatalf("refused link: %v", err)
		}
		var c shopeeConnectionJSON
		a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
		if c.Status != "invalid" {
			t.Fatalf("%+v", c)
		}
		if _, err := svc.UserCredential(ctx, ana); !errors.Is(err, domain.ErrNoCredential) {
			t.Fatalf("invalid credential used: %v", err)
		}
		if ok, _ := a.svcs.affiliator.Connected(ctx, ana); ok {
			t.Fatal("invalid credential counts as connected")
		}
		if err := svc.RecordFailure(ctx, ana, &shopee.APIError{Code: 10031}); err != nil {
			t.Fatal(err)
		}
		a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
		if c.Status != "expired" {
			t.Fatalf("%+v", c)
		}
		if err := svc.RecordFailure(ctx, ana, domain.ErrSourceUnavailable); err != nil {
			t.Fatal(err)
		}
		a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
		if c.Status != "expired" {
			t.Fatalf("an outage changed the status: %+v", c)
		}
	})

	a.must("ana", http.MethodDelete, "/v1/me/shopee", nil, nil, http.StatusNoContent)
	a.must("ana", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
	if c.Status != "disconnected" || len(raw("SELECT encrypted_secret FROM shopee_credentials")) != 0 {
		t.Fatalf("after disconnecting: %+v", c)
	}
	a.must("ana", http.MethodDelete, "/v1/me/shopee", nil, nil, http.StatusNoContent)

	if logs.Len() == 0 {
		t.Fatal("nothing was logged: the log check below proves nothing")
	}
	if strings.Contains(logs.String(), shopeeSecret) || strings.Contains(logs.String(), "new-secret") {
		t.Fatal("secret in the log")
	}
}
