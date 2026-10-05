package repository_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
)

func TestPostgresAuth(t *testing.T) {
	runAuthContract(t, repository.NewPostgresAuth(dbtest.New(t)))
}

// TestPostgresAuthIsolation checks the RLS policies: without the matching
// scope, accounts, sessions and tokens stay invisible.
func TestPostgresAuthIsolation(t *testing.T) {
	pool := dbtest.New(t)
	repo := repository.NewPostgresAuth(pool)
	ctx := context.Background()

	ana, err := repo.CreateAccount(ctx, domain.AuthAccount{Email: "ana@example.com", Name: "Ana", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	bia, err := repo.CreateAccount(ctx, domain.AuthAccount{Email: "bia@example.com", Name: "Bia", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	hash := []byte("0123456789abcdef0123456789abcdef")
	if err := repo.CreateSession(ctx, domain.AuthSession{TokenHash: hash, AccountID: ana.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	count := func(s database.Scope, query string) int {
		t.Helper()
		var n int
		err := database.InTx(ctx, pool, s, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, query).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	internal := func(id uuid.UUID) database.Scope {
		return database.Scope{AuthProvider: domain.AuthProviderInternal, AuthSubject: id.String()}
	}
	for name, tc := range map[string]struct {
		scope database.Scope
		query string
		want  int
	}{
		"no scope sees no account":           {database.Scope{}, "SELECT count(*) FROM auth_accounts", 0},
		"email scope sees its account":       {database.Scope{AuthEmail: "bia@example.com"}, "SELECT count(*) FROM auth_accounts", 1},
		"account sees only itself":           {internal(bia.ID), "SELECT count(*) FROM auth_accounts", 1},
		"other account sees no session":      {internal(bia.ID), "SELECT count(*) FROM auth_sessions", 0},
		"owner sees its session":             {internal(ana.ID), "SELECT count(*) FROM auth_sessions", 1},
		"session hash sees session":          {database.Scope{SessionHash: hex.EncodeToString(hash)}, "SELECT count(*) FROM auth_sessions", 1},
		"session hash sees only its account": {database.Scope{SessionHash: hex.EncodeToString(hash)}, "SELECT count(*) FROM auth_accounts", 1},
		"oidc subject is not an account":     {database.Scope{AuthProvider: domain.AuthProviderOIDC, AuthSubject: ana.ID.String()}, "SELECT count(*) FROM auth_sessions", 0},
	} {
		if got := count(tc.scope, tc.query); got != tc.want {
			t.Errorf("%s: %d rows, want %d", name, got, tc.want)
		}
	}

	// Another account cannot open sessions for Ana.
	err = database.InTx(ctx, pool, internal(bia.ID), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO auth_sessions (token_hash, account_id, expires_at) VALUES ('\x01', $1, now())`, ana.ID)
		return err
	})
	if err == nil {
		t.Fatal("session created for another account")
	}
}
