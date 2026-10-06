package queue

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// River enqueues jobs and implements the queue interfaces of the domain, one
// file per module (e.g. catalog.go). It inserts outside the transaction of the
// caller: the parceiros_app role has no access to River's tables, so the jobs
// must tolerate records that were rolled back (the workers check them).
//
// Client may stay nil inside the worker: the jobs then use the client of the
// job being worked.
type River struct {
	Client *river.Client[pgx.Tx]
}

func (r *River) client(ctx context.Context) (*river.Client[pgx.Tx], error) {
	if r != nil && r.Client != nil {
		return r.Client, nil
	}
	c, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return nil, errors.New("queue: no River client")
	}
	return c, nil
}

// insert enqueues the jobs.
func (r *River) insert(ctx context.Context, params ...river.InsertManyParams) error {
	if len(params) == 0 {
		return nil
	}
	c, err := r.client(ctx)
	if err != nil {
		return err
	}
	_, err = c.InsertMany(ctx, params)
	return err
}
