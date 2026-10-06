package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// Transactor implements domain.Transactor: it opens a transaction with the
// AppRole role and carries it in the context, so every repository call made
// with that context joins it.
type Transactor struct {
	Pool *pgxpool.Pool
}

// WithinTx runs fn in one transaction. A nested call joins the outer one.
func (t Transactor) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	return pgx.BeginFunc(ctx, t.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+AppRole); err != nil {
			return fmt.Errorf("assuming role %s: %w", AppRole, err)
		}
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// TxFromContext returns the transaction opened by Transactor.WithinTx.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// Run runs fn with the scope s. Inside Transactor.WithinTx it joins that
// transaction and replaces its scope; otherwise it opens one of its own.
func Run(ctx context.Context, pool *pgxpool.Pool, s Scope, fn func(pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		if err := SetScope(ctx, tx, s); err != nil {
			return err
		}
		return fn(tx)
	}
	return InTx(ctx, pool, s, fn)
}
