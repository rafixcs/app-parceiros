// Package dbtest gives integration tests a real Postgres with the migrations
// applied. Each call to New returns a fresh database, cloned from a template
// migrated once per package.
//
// By default Postgres runs with testcontainers (requires Docker). To use an
// existing server, set TEST_DATABASE_URL to a user that can create databases;
// the databases are not dropped, so use a throwaway server.
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/rafixcs/app-parceiros/backend/db"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
)

var (
	template = fmt.Sprintf("parceiros_template_%d", os.Getpid())
	once     sync.Once
	baseURL  string
	initErr  error
	seq      atomic.Int64
)

// New creates an isolated database for the test and returns a pool connected to it.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test with Postgres (run without -short)")
	}
	once.Do(func() { baseURL, initErr = setup() })
	if initErr != nil {
		t.Fatalf("preparing test Postgres: %v", initErr)
	}

	ctx := context.Background()
	name := fmt.Sprintf("t_%d_%d", os.Getpid(), seq.Add(1))
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+template); err != nil {
		t.Fatal(err)
	}

	pool, err := database.Open(ctx, withDB(baseURL, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func setup() (string, error) {
	ctx := context.Background()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("parceiros"),
			tcpostgres.WithUsername("parceiros"),
			tcpostgres.WithPassword("parceiros"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			return "", err
		}
		// Ryuk removes the container when the test process exits.
		base, err = c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return "", err
		}
	}

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", err
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+template); err != nil {
		return "", err
	}

	pool, err := database.Open(ctx, withDB(base, template))
	if err != nil {
		return "", err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return "", err
	}
	return base, nil
}

func withDB(raw, name string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = "/" + name
	return u.String()
}
