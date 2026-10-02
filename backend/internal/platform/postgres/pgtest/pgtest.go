// Package pgtest dá aos testes de integração um Postgres real com as
// migrations aplicadas. Cada chamada a New devolve um banco novo, clonado de
// um template migrado uma única vez por pacote.
//
// Por padrão o Postgres sobe com testcontainers (requer Docker). Para usar um
// servidor já existente, defina TEST_DATABASE_URL com um usuário que possa
// criar bancos; os bancos criados não são apagados, então use um servidor
// descartável.
package pgtest

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
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
)

var (
	template = fmt.Sprintf("parceiros_template_%d", os.Getpid())
	once     sync.Once
	baseURL  string
	initErr  error
	seq      atomic.Int64
)

// New cria um banco isolado para o teste e devolve um pool conectado a ele.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("teste de integração com Postgres (rode sem -short)")
	}
	once.Do(func() { baseURL, initErr = setup() })
	if initErr != nil {
		t.Fatalf("preparando Postgres de teste: %v", initErr)
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

	pool, err := postgres.Open(ctx, withDB(baseURL, name))
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
		// O container é removido pelo Ryuk quando o processo de teste termina.
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

	pool, err := postgres.Open(ctx, withDB(base, template))
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
