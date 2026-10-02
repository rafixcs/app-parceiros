package colecoes

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes/colecoesdb"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
)

// GerarLinkArgs gera os links de afiliado de um item, um por canal, com a
// credencial do dono do item.
type GerarLinkArgs struct {
	ItemID      uuid.UUID `json:"item_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UsuarioID   uuid.UUID `json:"usuario_id"`
}

func (GerarLinkArgs) Kind() string { return "gerar_link" }

func (GerarLinkArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       jobs.FilaShopee,
		MaxAttempts: 6,
		// Um job por item na fila de cada vez. Depois de concluído, pedir de
		// novo gera outro.
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

func (f FilaRiver) Enfileirar(ctx context.Context, args ...GerarLinkArgs) error {
	params := make([]river.InsertManyParams, len(args))
	for i, a := range args {
		params[i] = river.InsertManyParams{Args: a}
	}
	_, err := f.Client.InsertMany(ctx, params)
	return err
}

type GerarLinkWorker struct {
	river.WorkerDefaults[GerarLinkArgs]
	Svc *Service
	Log *slog.Logger
}

func (w *GerarLinkWorker) Work(ctx context.Context, job *river.Job[GerarLinkArgs]) error {
	d := Dono{WorkspaceID: job.Args.WorkspaceID, UsuarioID: job.Args.UsuarioID}
	log := w.Log.With("job", job.Kind, "item_id", job.Args.ItemID)

	row, err := w.Svc.item(ctx, d, job.Args.ItemID)
	if errors.Is(err, ErrItemNaoEncontrado) {
		return nil // o item foi removido
	}
	if err != nil {
		return err
	}
	if row.LinkOrigem == colecoesdb.LinkOrigemManual {
		return nil // o usuário gravou um link manual
	}
	prods, err := w.Svc.produtos.Varios(ctx, d.escopo(), []uuid.UUID{row.ProdutoID})
	if err != nil {
		return err
	}
	p, ok := prods[row.ProdutoID]
	if !ok {
		return river.JobCancel(errors.New("produto do item não está no catálogo"))
	}

	links := make([]LinkCanal, 0, len(Canais))
	for _, c := range Canais {
		subIDs := SubIDs(c, d.WorkspaceID)
		url, err := w.Svc.afiliador.GerarLink(ctx, d.UsuarioID, p.URL, subIDs)
		switch {
		case errors.Is(err, fontes.ErrSemCredencial),
			errors.Is(err, fontes.ErrCredencialInvalida),
			errors.Is(err, fontes.ErrAcessoNegado):
			// Fica pendente até o usuário conectar (ou reconectar) a Shopee.
			log.Info("sem credencial válida; link fica pendente", "err", err)
			return w.Svc.marcar(ctx, d, row.ID, LinkPendente)
		case errors.Is(err, fontes.ErrLimite):
			return river.JobSnooze(time.Minute)
		case err != nil:
			if job.Attempt >= job.MaxAttempts {
				log.Error("shopee não gerou o link; desistindo", "err", err)
				if errMarcar := w.Svc.marcar(ctx, d, row.ID, LinkFalhou); errMarcar != nil {
					return errors.Join(err, errMarcar)
				}
			}
			return err
		}
		links = append(links, LinkCanal{Canal: c, SubID: juntarSubIDs(subIDs), URL: url})
	}
	return w.Svc.concluir(ctx, d, row.ID, links)
}

// juntarSubIDs guarda os subIds como a Shopee os devolve no relatório de
// conversões (utmContent): separados por hífen.
func juntarSubIDs(s []string) string { return strings.Join(s, "-") }

// concluir grava os links por canal e o principal (canal Outro).
func (s *Service) concluir(ctx context.Context, d Dono, id uuid.UUID, links []LinkCanal) error {
	return s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var principal string
		for _, l := range links {
			if err := q.SalvarLinkCanal(ctx, colecoesdb.SalvarLinkCanalParams{
				ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID,
				Canal: colecoesdb.Canal(l.Canal), SubID: l.SubID, Url: l.URL,
			}); err != nil {
				return err
			}
			if l.Canal == CanalOutro {
				principal = l.URL
			}
		}
		_, err := q.ConcluirLink(ctx, colecoesdb.ConcluirLinkParams{
			ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, LinkAfiliado: &principal,
		})
		return err
	})
}
