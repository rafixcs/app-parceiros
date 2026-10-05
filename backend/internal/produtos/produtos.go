// Package produtos guarda o catálogo global (produtos, categorias e o
// histórico em produto_snapshots) coletado das fontes com a credencial do app.
//
// O catálogo não é dado de cliente. A API só lê, com o papel parceiros_app
// (database.InTx); o worker escreve com o papel dono das tabelas, porque cria
// as partições mensais dos snapshots.
package produtos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos/produtosdb"
)

type Produto struct {
	ID               uuid.UUID
	Fonte            fontes.Fonte
	ItemID           int64
	LojaNome         string
	Nome             string
	ImagemURL        *string
	CategoriaID      *int64
	Categorias       []int64
	URL              string
	PrecoMinCentavos int64
	PrecoMaxCentavos int64
	ComissaoBP       int32
	Vendas           int64
	Nota             *float64
	ColetadoEm       time.Time
}

type Snapshot struct {
	ColetadoEm       time.Time `json:"coletado_em"`
	PrecoMinCentavos int64     `json:"preco_min_centavos"`
	PrecoMaxCentavos int64     `json:"preco_max_centavos"`
	ComissaoBP       int32     `json:"comissao_bp"`
	Vendas           int64     `json:"vendas"`
	Nota             *float64  `json:"nota"`
}

// Atual são os dados mais recentes de um produto e o ponto de partida para
// medir o crescimento de vendas (veja ParaTendencias).
type Atual struct {
	Produto
	BaseVendas     int64
	BaseColetadoEm time.Time
}

type Categoria struct {
	ID        int64
	Nome      string
	Monitorar bool
}

var ErrProdutoNaoEncontrado = errors.New("produto não encontrado")

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// worker roda fn numa transação com o papel dono das tabelas.
func (s *Service) worker(ctx context.Context, fn func(*produtosdb.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(produtosdb.New(tx)) })
}

func (s *Service) leitura(ctx context.Context, e database.Scope, fn func(*produtosdb.Queries) error) error {
	return database.InTx(ctx, s.pool, e, func(tx pgx.Tx) error { return fn(produtosdb.New(tx)) })
}

// Registrar grava as ofertas de uma coleta: atualiza os dados atuais de cada
// produto e acrescenta um snapshot. Coletas na mesma hora cheia contam como
// uma só. Uso exclusivo do worker.
func (s *Service) Registrar(ctx context.Context, fonte fontes.Fonte, coletadoEm time.Time, ofertas []fontes.Oferta) error {
	coletadoEm = coletadoEm.UTC().Truncate(time.Hour)
	if len(ofertas) == 0 {
		return nil
	}
	return s.worker(ctx, func(q *produtosdb.Queries) error {
		if err := q.GarantirParticaoSnapshots(ctx, coletadoEm); err != nil {
			return fmt.Errorf("criando partição de snapshots: %w", err)
		}
		params := make([]produtosdb.UpsertProdutoParams, len(ofertas))
		for i, o := range ofertas {
			var cat *int64
			if len(o.Categorias) > 0 {
				cat = &o.Categorias[0]
			}
			var img *string
			if o.ImagemURL != "" {
				img = &o.ImagemURL
			}
			categorias := o.Categorias
			if categorias == nil {
				categorias = []int64{}
			}
			params[i] = produtosdb.UpsertProdutoParams{
				Fonte: produtosdb.Fonte(fonte), ItemID: o.ItemID, LojaID: o.LojaID, LojaNome: o.LojaNome,
				Nome: o.Nome, ImagemUrl: img, CategoriaID: cat, Categorias: categorias, Url: o.URL,
				PrecoMinCentavos: o.PrecoMinCentavos, PrecoMaxCentavos: o.PrecoMaxCentavos,
				ComissaoBp: o.ComissaoBP, Vendas: o.Vendas, Nota: o.Nota, ColetadoEm: coletadoEm,
			}
		}

		ids := make([]uuid.UUID, len(ofertas))
		var errBatch error
		q.UpsertProduto(ctx, params).QueryRow(func(i int, id uuid.UUID, err error) {
			switch {
			case err == nil:
				ids[i] = id
			case errors.Is(err, pgx.ErrNoRows):
				// Já existe uma coleta mais nova; o produto fica como está.
			case errBatch == nil:
				errBatch = err
			}
		})
		if errBatch != nil {
			return errBatch
		}
		for i, id := range ids {
			if id != uuid.Nil {
				continue
			}
			if ids[i], errBatch = q.ProdutoPorItem(ctx, produtosdb.ProdutoPorItemParams{
				Fonte: produtosdb.Fonte(fonte), ItemID: ofertas[i].ItemID,
			}); errBatch != nil {
				return errBatch
			}
		}

		snaps := make([]produtosdb.InserirSnapshotParams, len(ofertas))
		for i, o := range ofertas {
			snaps[i] = produtosdb.InserirSnapshotParams{
				ProdutoID: ids[i], ColetadoEm: coletadoEm,
				PrecoMinCentavos: o.PrecoMinCentavos, PrecoMaxCentavos: o.PrecoMaxCentavos,
				ComissaoBp: o.ComissaoBP, Vendas: o.Vendas, Nota: o.Nota,
			}
		}
		q.InserirSnapshot(ctx, snaps).Exec(func(_ int, err error) {
			if err != nil && errBatch == nil {
				errBatch = err
			}
		})
		return errBatch
	})
}

// CategoriasMonitoradas lista as categorias com snapshot periódico.
func (s *Service) CategoriasMonitoradas(ctx context.Context, fonte fontes.Fonte) ([]int64, error) {
	var out []int64
	err := s.worker(ctx, func(q *produtosdb.Queries) error {
		var err error
		out, err = q.CategoriasMonitoradas(ctx, produtosdb.Fonte(fonte))
		return err
	})
	return out, err
}

// SalvarCategorias cadastra ou atualiza categorias. Uso do worker.
func (s *Service) SalvarCategorias(ctx context.Context, fonte fontes.Fonte, cs []Categoria) error {
	return s.worker(ctx, func(q *produtosdb.Queries) error {
		for _, c := range cs {
			if err := q.UpsertCategoria(ctx, produtosdb.UpsertCategoriaParams{
				Fonte: produtosdb.Fonte(fonte), ID: c.ID, Nome: c.Nome, Monitorar: c.Monitorar,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ParaTendencias devolve os produtos coletados desde `desde`, com a base de
// comparação: o snapshot mais recente com 7 a 14 dias ou, sem ele, o mais
// antigo dos últimos 7 dias (que pode ser o próprio snapshot atual).
func (s *Service) ParaTendencias(ctx context.Context, desde time.Time) ([]Atual, error) {
	var out []Atual
	err := s.worker(ctx, func(q *produtosdb.Queries) error {
		rows, err := q.ParaTendencias(ctx, desde)
		if err != nil {
			return err
		}
		out = make([]Atual, len(rows))
		for i, r := range rows {
			out[i] = Atual{
				Produto: Produto{
					ID: r.ID, Fonte: fontes.Shopee, LojaNome: r.LojaNome, Nome: r.Nome, ImagemURL: r.ImagemUrl,
					CategoriaID: r.CategoriaID, Categorias: r.Categorias, URL: r.Url,
					PrecoMinCentavos: r.PrecoMinCentavos, PrecoMaxCentavos: r.PrecoMaxCentavos,
					ComissaoBP: r.ComissaoBp, Vendas: r.Vendas, Nota: r.Nota, ColetadoEm: r.ColetadoEm,
				},
				BaseVendas:     r.BaseVendas,
				BaseColetadoEm: r.BaseColetadoEm,
			}
		}
		return nil
	})
	return out, err
}

// GanhoPorVenda é o preço × a comissão, em centavos, arredondado.
func GanhoPorVenda(precoCentavos int64, comissaoBP int32) int64 {
	return (precoCentavos*int64(comissaoBP) + 5000) / 10000
}

// GanhoPorVendaCentavos é quanto o afiliado ganha numa venda pelo preço mínimo.
func (p Produto) GanhoPorVendaCentavos() int64 {
	return GanhoPorVenda(p.PrecoMinCentavos, p.ComissaoBP)
}

func produtoDe(p produtosdb.Produto) Produto {
	return Produto{
		ID: p.ID, Fonte: fontes.Fonte(p.Fonte), ItemID: p.ItemID, LojaNome: p.LojaNome, Nome: p.Nome,
		ImagemURL: p.ImagemUrl, CategoriaID: p.CategoriaID, Categorias: p.Categorias, URL: p.Url,
		PrecoMinCentavos: p.PrecoMinCentavos, PrecoMaxCentavos: p.PrecoMaxCentavos,
		ComissaoBP: p.ComissaoBp, Vendas: p.Vendas, Nota: p.Nota, ColetadoEm: p.ColetadoEm,
	}
}

// Varios lê vários produtos do catálogo de uma vez (API). Ids desconhecidos
// ficam de fora do mapa.
func (s *Service) Varios(ctx context.Context, e database.Scope, ids []uuid.UUID) (map[uuid.UUID]Produto, error) {
	out := make(map[uuid.UUID]Produto, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	err := s.leitura(ctx, e, func(q *produtosdb.Queries) error {
		rows, err := q.Produtos(ctx, ids)
		for _, p := range rows {
			out[p.ID] = produtoDe(p)
		}
		return err
	})
	return out, err
}

// PorItem acha um produto do catálogo pelo ID na fonte (API). Devolve
// ErrProdutoNaoEncontrado se ele ainda não foi coletado.
func (s *Service) PorItem(ctx context.Context, e database.Scope, fonte fontes.Fonte, itemID int64) (Produto, error) {
	var p produtosdb.Produto
	err := s.leitura(ctx, e, func(q *produtosdb.Queries) error {
		var err error
		p, err = q.ProdutoPorItemCompleto(ctx, produtosdb.ProdutoPorItemCompletoParams{Fonte: produtosdb.Fonte(fonte), ItemID: itemID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Produto{}, ErrProdutoNaoEncontrado
	}
	if err != nil {
		return Produto{}, err
	}
	return produtoDe(p), nil
}

// Importar grava no catálogo um produto que o usuário trouxe colando o link,
// buscado na fonte com a credencial do app. É a única escrita da API no
// catálogo, com o papel dono das tabelas, como o worker.
func (s *Service) Importar(ctx context.Context, fonte fontes.Fonte, o fontes.Oferta, agora time.Time) (Produto, error) {
	if err := s.Registrar(ctx, fonte, agora, []fontes.Oferta{o}); err != nil {
		return Produto{}, err
	}
	var p produtosdb.Produto
	err := s.worker(ctx, func(q *produtosdb.Queries) error {
		var err error
		p, err = q.ProdutoPorItemCompleto(ctx, produtosdb.ProdutoPorItemCompletoParams{Fonte: produtosdb.Fonte(fonte), ItemID: o.ItemID})
		return err
	})
	if err != nil {
		return Produto{}, err
	}
	return produtoDe(p), nil
}

// Produto lê um produto do catálogo (API).
func (s *Service) Produto(ctx context.Context, e database.Scope, id uuid.UUID) (Produto, error) {
	var p produtosdb.Produto
	err := s.leitura(ctx, e, func(q *produtosdb.Queries) error {
		var err error
		p, err = q.Produto(ctx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Produto{}, ErrProdutoNaoEncontrado
	}
	if err != nil {
		return Produto{}, err
	}
	return produtoDe(p), nil
}

// Historico devolve os snapshots do produto desde `desde`, do mais antigo ao
// mais novo (API).
func (s *Service) Historico(ctx context.Context, e database.Scope, id uuid.UUID, desde time.Time) ([]Snapshot, error) {
	var out []Snapshot
	err := s.leitura(ctx, e, func(q *produtosdb.Queries) error {
		rows, err := q.Historico(ctx, produtosdb.HistoricoParams{ProdutoID: id, Desde: desde})
		if err != nil {
			return err
		}
		out = make([]Snapshot, len(rows))
		for i, r := range rows {
			out[i] = Snapshot{
				ColetadoEm: r.ColetadoEm, PrecoMinCentavos: r.PrecoMinCentavos, PrecoMaxCentavos: r.PrecoMaxCentavos,
				ComissaoBP: r.ComissaoBp, Vendas: r.Vendas, Nota: r.Nota,
			}
		}
		return nil
	})
	return out, err
}

// NomesCategorias devolve o nome das categorias conhecidas entre ids (API).
func (s *Service) NomesCategorias(ctx context.Context, e database.Scope, fonte fontes.Fonte, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	err := s.leitura(ctx, e, func(q *produtosdb.Queries) error {
		rows, err := q.NomesCategorias(ctx, produtosdb.NomesCategoriasParams{Fonte: produtosdb.Fonte(fonte), Ids: ids})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.ID] = r.Nome
		}
		return nil
	})
	return out, err
}
