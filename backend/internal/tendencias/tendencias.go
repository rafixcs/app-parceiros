// Package tendencias calcula o score de tendência dos produtos e serve o
// radar. Mantém a tabela tendencias, uma cópia dos dados de exibição do
// produto mais o score, para filtrar e ordenar o radar sem ler as tabelas do
// módulo produtos (que ele consulta pela interface pública).
package tendencias

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/tendencias/tendenciasdb"
)

// JanelaAtivos: produtos sem coleta há mais tempo que isso saem do radar.
const JanelaAtivos = 48 * time.Hour

type Item struct {
	ProdutoID             uuid.UUID `json:"produto_id"`
	Nome                  string    `json:"nome"`
	ImagemURL             *string   `json:"imagem_url"`
	LojaNome              string    `json:"loja_nome"`
	URL                   string    `json:"url"`
	Categorias            []int64   `json:"categorias"`
	PrecoMinCentavos      int64     `json:"preco_min_centavos"`
	PrecoMaxCentavos      int64     `json:"preco_max_centavos"`
	ComissaoBP            int32     `json:"comissao_bp"`
	GanhoPorVendaCentavos int64     `json:"ganho_por_venda_centavos"`
	Vendas                int64     `json:"vendas"`
	Nota                  *float64  `json:"nota"`
	Score                 float64   `json:"score"`
	Vendas7d              *int64    `json:"vendas_7d"`
	AtualizadoEm          time.Time `json:"atualizado_em"`
}

type Pagina struct {
	Itens        []Item     `json:"itens"`
	Total        int64      `json:"total"`
	Pagina       int        `json:"pagina"`
	PorPagina    int        `json:"por_pagina"`
	AtualizadoEm *time.Time `json:"atualizado_em"`
}

type Detalhe struct {
	Produto   Item                `json:"produto"`
	Historico []produtos.Snapshot `json:"historico"`
}

type Categoria struct {
	ID       int64  `json:"id"`
	Nome     string `json:"nome"`
	Produtos int64  `json:"produtos"`
}

type Ordem string

const (
	OrdemTendencia Ordem = "tendencia"
	OrdemComissao  Ordem = "comissao"
	OrdemGanho     Ordem = "ganho"
	OrdemVendas    Ordem = "vendas"
)

type Filtro struct {
	Categoria   *int64
	PrecoMin    *int64
	PrecoMax    *int64
	ComissaoMin *int32
	NotaMin     *float64
	Busca       string
	Ordem       Ordem
	Pagina      int
	PorPagina   int
}

const (
	PorPaginaPadrao = 24
	PorPaginaMax    = 50
	tamanhoBuscaMax = 100
)

var ErrProdutoNaoEncontrado = errors.New("produto não encontrado")

type ErrFiltro struct{ Mensagem string }

func (e *ErrFiltro) Error() string { return e.Mensagem }

type Service struct {
	pool     *pgxpool.Pool
	produtos *produtos.Service
}

func NewService(pool *pgxpool.Pool, p *produtos.Service) *Service {
	return &Service{pool: pool, produtos: p}
}

// Calcular recalcula o score de todos os produtos coletados nas últimas
// JanelaAtivos e tira do radar os que não foram. Uso do worker.
func (s *Service) Calcular(ctx context.Context, agora time.Time) (int, error) {
	atuais, err := s.produtos.ParaTendencias(ctx, agora.Add(-JanelaAtivos))
	if err != nil {
		return 0, fmt.Errorf("lendo produtos: %w", err)
	}
	v7s := make([]*int64, len(atuais))
	brutos := make([]float64, len(atuais))
	for i, a := range atuais {
		v7s[i] = Vendas7d(Entrada{
			Vendas: a.Vendas, ColetadoEm: a.ColetadoEm,
			BaseVendas: a.BaseVendas, BaseColetadoEm: a.BaseColetadoEm,
			ComissaoBP: a.ComissaoBP, Nota: a.Nota,
		})
		brutos[i] = Bruto(v7s[i], a.ComissaoBP, a.Nota)
	}
	scores := Normalizar(brutos)

	params := make([]tendenciasdb.UpsertTendenciaParams, len(atuais))
	for i, a := range atuais {
		params[i] = tendenciasdb.UpsertTendenciaParams{
			ProdutoID: a.ID, CalculadoEm: agora, Score: scores[i],
			GanhoPorVendaCentavos: GanhoPorVenda(a.PrecoMinCentavos, a.ComissaoBP),
			VariacaoVendas7d:      v7s[i],
			Nome:                  a.Nome, LojaNome: a.LojaNome, ImagemUrl: a.ImagemURL, CategoriaID: a.CategoriaID,
			Categorias: a.Categorias, Url: a.URL, PrecoMinCentavos: a.PrecoMinCentavos,
			PrecoMaxCentavos: a.PrecoMaxCentavos, ComissaoBp: a.ComissaoBP, Vendas: a.Vendas,
			Nota: a.Nota, AtualizadoEm: a.ColetadoEm,
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := tendenciasdb.New(tx)
		var errBatch error
		q.UpsertTendencia(ctx, params).Exec(func(_ int, err error) {
			if err != nil && errBatch == nil {
				errBatch = err
			}
		})
		if errBatch != nil {
			return errBatch
		}
		_, err := q.RemoverTendenciasAntigas(ctx, agora)
		return err
	})
	return len(atuais), err
}

// Radar lista o radar com filtros e ordenação.
func (s *Service) Radar(ctx context.Context, e database.Scope, f Filtro) (Pagina, error) {
	f, p, err := validar(f)
	if err != nil {
		return Pagina{}, err
	}
	out := Pagina{Itens: []Item{}, Pagina: f.Pagina, PorPagina: f.PorPagina}
	err = database.InTx(ctx, s.pool, e, func(tx pgx.Tx) error {
		q := tendenciasdb.New(tx)
		rows, err := q.Radar(ctx, p)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out.Total = r.Total
			out.Itens = append(out.Itens, Item{
				ProdutoID: r.ProdutoID, Nome: r.Nome, ImagemURL: r.ImagemUrl, LojaNome: r.LojaNome, URL: r.Url,
				Categorias: r.Categorias, PrecoMinCentavos: r.PrecoMinCentavos, PrecoMaxCentavos: r.PrecoMaxCentavos,
				ComissaoBP: r.ComissaoBp, GanhoPorVendaCentavos: r.GanhoPorVendaCentavos, Vendas: r.Vendas,
				Nota: r.Nota, Score: r.Score, Vendas7d: r.VariacaoVendas7d, AtualizadoEm: r.AtualizadoEm,
			})
		}
		at, err := q.RadarAtualizadoEm(ctx)
		if err == nil {
			out.AtualizadoEm = &at
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	return out, err
}

// validar confere o filtro, preenche os padrões e monta os parâmetros da query.
func validar(f Filtro) (Filtro, tendenciasdb.RadarParams, error) {
	if f.Pagina == 0 {
		f.Pagina = 1
	}
	if f.PorPagina == 0 {
		f.PorPagina = PorPaginaPadrao
	}
	switch {
	case f.Pagina < 1 || f.Pagina > 1000:
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{"A página deve ficar entre 1 e 1000."}
	case f.PorPagina < 1 || f.PorPagina > PorPaginaMax:
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{fmt.Sprintf("Mostre de 1 a %d produtos por página.", PorPaginaMax)}
	case f.PrecoMin != nil && *f.PrecoMin < 0, f.PrecoMax != nil && *f.PrecoMax < 0:
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{"O preço não pode ser negativo."}
	case f.ComissaoMin != nil && (*f.ComissaoMin < 0 || *f.ComissaoMin > 10000):
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{"A comissão mínima deve ficar entre 0% e 100%."}
	case f.NotaMin != nil && (*f.NotaMin < 0 || *f.NotaMin > 5):
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{"A nota mínima deve ficar entre 0 e 5."}
	}
	switch f.Ordem {
	case "":
		f.Ordem = OrdemTendencia
	case OrdemTendencia, OrdemComissao, OrdemGanho, OrdemVendas:
	default:
		return f, tendenciasdb.RadarParams{}, &ErrFiltro{"Ordenação desconhecida."}
	}
	p := tendenciasdb.RadarParams{
		Categoria: f.Categoria, PrecoMin: f.PrecoMin, PrecoMax: f.PrecoMax, ComissaoMin: f.ComissaoMin,
		NotaMin: f.NotaMin, Ordem: string(f.Ordem),
		Limite: int32(f.PorPagina), Deslocamento: int32((f.Pagina - 1) * f.PorPagina),
	}
	if b := strings.TrimSpace(f.Busca); b != "" {
		if len([]rune(b)) > tamanhoBuscaMax {
			return f, p, &ErrFiltro{fmt.Sprintf("A busca pode ter até %d caracteres.", tamanhoBuscaMax)}
		}
		padrao := "%" + escaparLike(b) + "%"
		p.Busca, p.Padrao = &b, &padrao
	}
	return f, p, nil
}

func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Produto devolve o item do radar com o histórico dos últimos `dias`. Um
// produto que saiu do radar ainda aparece, sem score.
func (s *Service) Produto(ctx context.Context, e database.Scope, id uuid.UUID, dias int, agora time.Time) (Detalhe, error) {
	if dias == 0 {
		dias = 30
	}
	if dias < 1 || dias > 90 {
		return Detalhe{}, &ErrFiltro{"O histórico pode cobrir de 1 a 90 dias."}
	}
	var out Detalhe
	err := database.InTx(ctx, s.pool, e, func(tx pgx.Tx) error {
		r, err := tendenciasdb.New(tx).RadarItem(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.Produto = Item{
			ProdutoID: r.ProdutoID, Nome: r.Nome, ImagemURL: r.ImagemUrl, LojaNome: r.LojaNome, URL: r.Url,
			Categorias: r.Categorias, PrecoMinCentavos: r.PrecoMinCentavos, PrecoMaxCentavos: r.PrecoMaxCentavos,
			ComissaoBP: r.ComissaoBp, GanhoPorVendaCentavos: r.GanhoPorVendaCentavos, Vendas: r.Vendas,
			Nota: r.Nota, Score: r.Score, Vendas7d: r.VariacaoVendas7d, AtualizadoEm: r.AtualizadoEm,
		}
		return nil
	})
	if err != nil {
		return Detalhe{}, err
	}
	if out.Produto.ProdutoID == uuid.Nil {
		p, err := s.produtos.Produto(ctx, e, id)
		if errors.Is(err, produtos.ErrProdutoNaoEncontrado) {
			return Detalhe{}, ErrProdutoNaoEncontrado
		}
		if err != nil {
			return Detalhe{}, err
		}
		out.Produto = Item{
			ProdutoID: p.ID, Nome: p.Nome, ImagemURL: p.ImagemURL, LojaNome: p.LojaNome, URL: p.URL,
			Categorias: p.Categorias, PrecoMinCentavos: p.PrecoMinCentavos, PrecoMaxCentavos: p.PrecoMaxCentavos,
			ComissaoBP: p.ComissaoBP, GanhoPorVendaCentavos: GanhoPorVenda(p.PrecoMinCentavos, p.ComissaoBP),
			Vendas: p.Vendas, Nota: p.Nota, AtualizadoEm: p.ColetadoEm,
		}
	}
	out.Historico, err = s.produtos.Historico(ctx, e, id, agora.Add(-time.Duration(dias)*24*time.Hour))
	return out, err
}

// Categorias lista as categorias de nível 1 presentes no radar, com nome
// quando conhecido.
func (s *Service) Categorias(ctx context.Context, e database.Scope) ([]Categoria, error) {
	var rows []tendenciasdb.CategoriasDoRadarRow
	err := database.InTx(ctx, s.pool, e, func(tx pgx.Tx) error {
		var err error
		rows, err = tendenciasdb.New(tx).CategoriasDoRadar(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	nomes, err := s.produtos.NomesCategorias(ctx, e, fontes.Shopee, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Categoria, len(rows))
	for i, r := range rows {
		nome, ok := nomes[r.ID]
		if !ok {
			nome = fmt.Sprintf("Categoria %d", r.ID)
		}
		out[i] = Categoria{ID: r.ID, Nome: nome, Produtos: r.Produtos}
	}
	return out, nil
}
