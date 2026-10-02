// Package jobs configura o River (fila de jobs no Postgres).
package jobs

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

// Filas definidas na stack. Cada uma tem seu limite de concorrência.
const (
	FilaDefault = river.QueueDefault
	FilaShopee  = "shopee"
	FilaMidia   = "midia"
)

// Migrate cria ou atualiza as tabelas internas do River.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrations do river: %w", err)
	}
	return nil
}

// NewWorkerClient cria um cliente River que processa as filas. Os módulos de
// domínio registram seus workers em `workers` antes da chamada.
func NewWorkerClient(pool *pgxpool.Pool, workers *river.Workers, periodicos []*river.PeriodicJob, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:       log,
		PeriodicJobs: periodicos,
		Queues: map[string]river.QueueConfig{
			FilaDefault: {MaxWorkers: 10},
			FilaShopee:  {MaxWorkers: 2},
			FilaMidia:   {MaxWorkers: 2},
		},
		Workers: workers,
	})
}

// PingArgs é um job de verificação: confirma de ponta a ponta que a API
// enfileira e o worker processa. O River exige ao menos um worker
// registrado, e este cobre o período antes dos módulos de domínio.
type PingArgs struct{}

func (PingArgs) Kind() string { return "ping" }

type PingWorker struct {
	river.WorkerDefaults[PingArgs]
	Log *slog.Logger
}

func (w *PingWorker) Work(_ context.Context, job *river.Job[PingArgs]) error {
	w.Log.Info("ping processado", "job_id", job.ID)
	return nil
}
