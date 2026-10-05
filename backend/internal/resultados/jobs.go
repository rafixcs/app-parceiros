package resultados

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
)

// IntervaloSync é a frequência da sincronização automática.
const IntervaloSync = 24 * time.Hour

// SyncConversoesArgs sincroniza as conversões de um usuário com a credencial
// dele.
type SyncConversoesArgs struct {
	UsuarioID uuid.UUID `json:"usuario_id"`
}

func (SyncConversoesArgs) Kind() string { return "sync_conversoes" }

func (SyncConversoesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       jobs.FilaShopee,
		MaxAttempts: 5,
		// Uma sincronização por usuário na fila de cada vez.
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

func (f FilaRiver) Enfileirar(ctx context.Context, args SyncConversoesArgs) error {
	_, err := f.Client.Insert(ctx, args, nil)
	return err
}

// Mensagens gravadas na sincronização que falhou. São fixas: o erro original
// pode trazer detalhes da chamada que não devem chegar ao usuário.
var (
	msgCredencial   = "A Shopee recusou a sua credencial. Conecte a conta de afiliado de novo."
	msgIndisponivel = "Não conseguimos falar com a Shopee. Tentaremos de novo na próxima sincronização."
)

type SyncConversoesWorker struct {
	river.WorkerDefaults[SyncConversoesArgs]
	Svc *Service
	Log *slog.Logger
}

// A paginação é sequencial e respeita o rate limit, então pode demorar.
func (w *SyncConversoesWorker) Timeout(*river.Job[SyncConversoesArgs]) time.Duration {
	return 15 * time.Minute
}

func (w *SyncConversoesWorker) Work(ctx context.Context, job *river.Job[SyncConversoesArgs]) error {
	log := w.Log.With("job", job.Kind, "usuario_id", job.Args.UsuarioID)
	n, err := w.Svc.Sincronizar(ctx, job.Args.UsuarioID)
	switch {
	case err == nil:
		log.Info("conversões sincronizadas", "conversoes", n)
		return nil
	case errors.Is(err, fontes.ErrLimite):
		// O scrollId expira: a próxima tentativa recomeça do início.
		return river.JobSnooze(5 * time.Minute)
	case errors.Is(err, fontes.ErrCredencialInvalida), errors.Is(err, fontes.ErrAcessoNegado):
		log.Info("credencial recusada; sincronização interrompida", "err", err)
		return w.Svc.concluir(ctx, job.Args.UsuarioID, SyncErro, 0, &msgCredencial)
	}
	if job.Attempt >= job.MaxAttempts {
		log.Error("sincronização falhou; desistindo", "err", err)
		if errReg := w.Svc.concluir(ctx, job.Args.UsuarioID, SyncErro, 0, &msgIndisponivel); errReg != nil {
			return errors.Join(err, errReg)
		}
	}
	return err
}

// AgendarSyncArgs é o job periódico que enfileira a sincronização de cada
// usuário com a Shopee conectada.
type AgendarSyncArgs struct{}

func (AgendarSyncArgs) Kind() string { return "agendar_sync_conversoes" }

// Conectados lista os usuários com credencial conectada
// (shopee.Credenciais.Conectados).
type Conectados interface {
	Conectados(ctx context.Context) ([]uuid.UUID, error)
}

type AgendarSyncWorker struct {
	river.WorkerDefaults[AgendarSyncArgs]
	Usuarios Conectados
}

func (w *AgendarSyncWorker) Work(ctx context.Context, _ *river.Job[AgendarSyncArgs]) error {
	ids, err := w.Usuarios.Conectados(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: SyncConversoesArgs{UsuarioID: id}}
	}
	_, err = river.ClientFromContext[pgx.Tx](ctx).InsertMany(ctx, params)
	return err
}

// PeriodicoSync agenda o AgendarSyncArgs uma vez por dia. Não roda ao subir o
// worker, para um deploy não disparar a sincronização de todos; o usuário
// pode pedir a dele pelo painel.
func PeriodicoSync() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(IntervaloSync),
		func() (river.JobArgs, *river.InsertOpts) { return AgendarSyncArgs{}, nil },
		&river.PeriodicJobOpts{ID: "agendar_sync_conversoes"},
	)
}
