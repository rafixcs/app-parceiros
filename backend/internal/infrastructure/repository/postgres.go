// Package repository implements the domain repositories: postgres_*.go on
// Postgres (with the RLS scope of each call) and inmem_*.go in memory, for
// service tests.
package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// run runs fn with the scope s, joining the transaction of the context when
// there is one (see database.Transactor).
func run(ctx context.Context, pool *pgxpool.Pool, s database.Scope, fn func(*dbgen.Queries, pgx.Tx) error) error {
	return database.Run(ctx, pool, s, func(tx pgx.Tx) error {
		return fn(dbgen.New(tx), tx)
	})
}

// scopeOf is the RLS scope of an actor.
func scopeOf(a domain.Actor) database.Scope {
	return database.Scope{UserID: idString(a.UserID), WorkspaceID: idString(a.WorkspaceID)}
}

// userScope is the scope of a user outside any workspace.
func userScope(id uuid.UUID) database.Scope { return database.Scope{UserID: idString(id)} }

// workspaceScope is the scope of a workspace without a user (webhooks and
// background jobs).
func workspaceScope(id uuid.UUID) database.Scope { return database.Scope{WorkspaceID: idString(id)} }

func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
