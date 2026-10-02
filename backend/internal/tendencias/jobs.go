package tendencias

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
)

// CalcularTendenciasArgs recalcula o radar. É enfileirado ao fim de cada
// snapshot, com um pequeno atraso para juntar as coletas que terminam juntas.
type CalcularTendenciasArgs struct{}

func (CalcularTendenciasArgs) Kind() string { return "calcular_tendencias" }

func (CalcularTendenciasArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: jobs.FilaDefault,
		// Um cálculo por janela de 5 minutos; o próximo snapshot que terminar
		// depois disso agenda outro.
		UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute},
	}
}

// Enfileirar agenda o cálculo para daqui a 30 segundos. Roda dentro de um job
// (usa o cliente River do contexto).
func Enfileirar(ctx context.Context) error {
	opts := CalcularTendenciasArgs{}.InsertOpts()
	opts.ScheduledAt = time.Now().Add(30 * time.Second)
	_, err := river.ClientFromContext[pgx.Tx](ctx).Insert(ctx, CalcularTendenciasArgs{}, &opts)
	return err
}

type CalcularTendenciasWorker struct {
	river.WorkerDefaults[CalcularTendenciasArgs]
	Svc *Service
	Log *slog.Logger
}

func (w *CalcularTendenciasWorker) Timeout(*river.Job[CalcularTendenciasArgs]) time.Duration {
	return 10 * time.Minute
}

func (w *CalcularTendenciasWorker) Work(ctx context.Context, job *river.Job[CalcularTendenciasArgs]) error {
	n, err := w.Svc.Calcular(ctx, time.Now())
	if err != nil {
		return err
	}
	w.Log.Info("tendências calculadas", "job", job.Kind, "produtos", n)
	return nil
}
