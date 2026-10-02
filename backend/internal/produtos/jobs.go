package produtos

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
)

// IntervaloSnapshot é a frequência do snapshot de cada categoria.
const IntervaloSnapshot = 6 * time.Hour

// SnapshotCatalogoArgs coleta uma categoria (0 = todas) do catálogo da fonte,
// página a página, até Paginas páginas.
type SnapshotCatalogoArgs struct {
	CategoriaID int64 `json:"categoria_id"`
	Paginas     int   `json:"paginas"`
}

func (SnapshotCatalogoArgs) Kind() string { return "snapshot_catalogo" }

func (SnapshotCatalogoArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       jobs.FilaShopee,
		MaxAttempts: 5,
		// Uma coleta por categoria por hora, mesmo se o agendador repetir.
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour},
	}
}

type SnapshotCatalogoWorker struct {
	river.WorkerDefaults[SnapshotCatalogoArgs]
	Svc      *Service
	Catalogo fontes.Catalogo
	Storage  storage.Storage
	Log      *slog.Logger
	// Depois roda ao fim de uma coleta com produtos, para enfileirar o
	// cálculo de tendências sem que este pacote dependa dele.
	Depois func(ctx context.Context) error
	Agora  func() time.Time
}

// A paginação é sequencial e respeita o rate limit, então pode demorar.
func (w *SnapshotCatalogoWorker) Timeout(*river.Job[SnapshotCatalogoArgs]) time.Duration {
	return 20 * time.Minute
}

func (w *SnapshotCatalogoWorker) Work(ctx context.Context, job *river.Job[SnapshotCatalogoArgs]) error {
	agora := time.Now
	if w.Agora != nil {
		agora = w.Agora
	}
	coletadoEm := agora().UTC().Truncate(time.Hour)
	paginas := job.Args.Paginas
	if paginas < 1 {
		paginas = 1
	}
	log := w.Log.With("job", job.Kind, "categoria", job.Args.CategoriaID)

	total := 0
	for pagina := 1; pagina <= paginas; pagina++ {
		p, err := w.Catalogo.Ofertas(ctx, fontes.FiltroCatalogo{
			CategoriaID: job.Args.CategoriaID, Pagina: pagina, Limite: 50,
		})
		switch {
		case errors.Is(err, fontes.ErrLimite):
			// As páginas já gravadas ficam; a coleta recomeça mais tarde.
			log.Warn("limite de chamadas da fonte; adiando a coleta", "pagina", pagina)
			return river.JobSnooze(5 * time.Minute)
		case errors.Is(err, fontes.ErrCredencialInvalida), errors.Is(err, fontes.ErrAcessoNegado):
			log.Error("a fonte recusou a credencial do app; confira SHOPEE_APP_ID e SHOPEE_APP_SECRET", "err", err)
			return river.JobCancel(err)
		case err != nil:
			return err
		}

		chave := fmt.Sprintf("%s/catalogo/%s/categoria-%d/pagina-%03d.json.gz",
			w.Catalogo.Fonte(), coletadoEm.Format("2006/01/02/15"), job.Args.CategoriaID, pagina)
		if err := w.guardarBruto(ctx, chave, p.Bruto); err != nil {
			// A resposta bruta serve para reprocessar; não vale perder a coleta por ela.
			log.Warn("não foi possível guardar a resposta bruta", "chave", chave, "err", err)
		}
		if err := w.Svc.Registrar(ctx, w.Catalogo.Fonte(), coletadoEm, p.Ofertas); err != nil {
			return fmt.Errorf("registrando página %d: %w", pagina, err)
		}
		total += len(p.Ofertas)
		if !p.TemProxima {
			break
		}
	}

	log.Info("coleta concluída", "produtos", total)
	if total > 0 && w.Depois != nil {
		return w.Depois(ctx)
	}
	return nil
}

func (w *SnapshotCatalogoWorker) guardarBruto(ctx context.Context, chave string, bruto []byte) error {
	if w.Storage == nil || len(bruto) == 0 {
		return nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(bruto); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return w.Storage.Guardar(ctx, chave, buf.Bytes(), "application/gzip")
}

// AgendarSnapshotsArgs é o job periódico que enfileira um snapshot por
// categoria monitorada, mais um geral (todas as categorias).
type AgendarSnapshotsArgs struct{}

func (AgendarSnapshotsArgs) Kind() string { return "agendar_snapshots" }

type AgendarSnapshotsWorker struct {
	river.WorkerDefaults[AgendarSnapshotsArgs]
	Svc     *Service
	Fonte   fontes.Fonte
	Paginas int
}

func (w *AgendarSnapshotsWorker) Work(ctx context.Context, _ *river.Job[AgendarSnapshotsArgs]) error {
	cats, err := w.Svc.CategoriasMonitoradas(ctx, w.Fonte)
	if err != nil {
		return err
	}
	params := []river.InsertManyParams{{Args: SnapshotCatalogoArgs{CategoriaID: 0, Paginas: w.Paginas}}}
	for _, c := range cats {
		params = append(params, river.InsertManyParams{Args: SnapshotCatalogoArgs{CategoriaID: c, Paginas: w.Paginas}})
	}
	_, err = river.ClientFromContext[pgx.Tx](ctx).InsertMany(ctx, params)
	return err
}

// PeriodicoSnapshots agenda o AgendarSnapshotsArgs a cada IntervaloSnapshot,
// e uma vez quando o worker sobe.
func PeriodicoSnapshots() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(IntervaloSnapshot),
		func() (river.JobArgs, *river.InsertOpts) { return AgendarSnapshotsArgs{}, nil },
		&river.PeriodicJobOpts{ID: "agendar_snapshots", RunOnStart: true},
	)
}
