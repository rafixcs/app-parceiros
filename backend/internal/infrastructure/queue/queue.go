// Package queue configures River (the job queue in Postgres).
package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Queues defined by the stack, each with its own concurrency limit.
const (
	QueueDefault = river.QueueDefault
	QueueShopee  = "shopee"
	QueueMedia   = "media"
)

// Migrate creates or updates River's own tables.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrations: %w", err)
	}
	return nil
}

// NewWorkerClient creates a River client that works the queues. The workers
// must be registered in `workers` before the call.
func NewWorkerClient(pool *pgxpool.Pool, workers *river.Workers, periodic []*river.PeriodicJob, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:       log,
		PeriodicJobs: periodic,
		Queues: map[string]river.QueueConfig{
			QueueDefault: {MaxWorkers: 10},
			QueueShopee:  {MaxWorkers: 2},
			QueueMedia:   {MaxWorkers: 2},
		},
		Workers: workers,
	})
}

// NewInsertClient creates a River client that only enqueues (the API).
func NewInsertClient(pool *pgxpool.Pool, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: log})
}

// PingArgs is a health job: it proves end to end that the API enqueues and
// the worker processes. River requires at least one registered worker.
type PingArgs struct{}

func (PingArgs) Kind() string { return "ping" }

type PingWorker struct {
	river.WorkerDefaults[PingArgs]
	Log *slog.Logger
}

func (w *PingWorker) Work(_ context.Context, job *river.Job[PingArgs]) error {
	w.Log.Info("ping processed", "job_id", job.ID)
	return nil
}
