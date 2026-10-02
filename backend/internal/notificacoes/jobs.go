package notificacoes

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
)

// EntregarArgs entrega uma notificação a um usuário. Leva só o conteúdo e os
// ids: o e-mail do destinatário é lido na hora da entrega.
type EntregarArgs struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UsuarioID   uuid.UUID `json:"usuario_id"`
	Tipo        string    `json:"tipo"`
	Chave       string    `json:"chave"`
	Titulo      string    `json:"titulo"`
	Corpo       string    `json:"corpo"`
	URL         string    `json:"url"`
}

func (EntregarArgs) Kind() string { return "entregar_notificacao" }

func (EntregarArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       jobs.FilaDefault,
		MaxAttempts: 8,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	}
}

// FilaRiver enfileira no River com um cliente só de inserção.
type FilaRiver struct {
	Client *river.Client[pgx.Tx]
}

func (f FilaRiver) Enfileirar(ctx context.Context, args ...EntregarArgs) error {
	params := make([]river.InsertManyParams, len(args))
	for i, a := range args {
		params[i] = river.InsertManyParams{Args: a}
	}
	_, err := f.Client.InsertMany(ctx, params)
	return err
}

type EntregarWorker struct {
	river.WorkerDefaults[EntregarArgs]
	Svc *Service
}

func (w *EntregarWorker) Work(ctx context.Context, job *river.Job[EntregarArgs]) error {
	return w.Svc.Entregar(ctx, job.Args)
}
