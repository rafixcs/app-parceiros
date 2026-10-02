// Package colecoes guarda os produtos que o afiliado salvou para divulgar
// (itens), com título, descrição, notas, tags, status e link de afiliado, e as
// coleções (pastas) em que ele os organiza.
//
// A coleção é do usuário dentro de cada workspace: nem o mentor a vê. O link
// automático sai do job gerar_link, com a credencial do próprio usuário e um
// subId por canal.
package colecoes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes/colecoesdb"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
)

type Status string

const (
	StatusTestando   Status = "testando"
	StatusCampeao    Status = "campeao"
	StatusDescartado Status = "descartado"
)

func (s Status) valido() bool {
	return s == StatusTestando || s == StatusCampeao || s == StatusDescartado
}

type Canal string

const (
	CanalInstagram Canal = "instagram"
	CanalTikTok    Canal = "tiktok"
	CanalWhatsApp  Canal = "whatsapp"
	CanalOutro     Canal = "outro"
)

// Canais recebem um link cada. O do canal Outro é o link principal do item.
var Canais = []Canal{CanalInstagram, CanalTikTok, CanalWhatsApp, CanalOutro}

// SubIDs marca o link com o canal e o workspace, para o relatório de
// conversões separar os resultados (E7).
func SubIDs(c Canal, workspaceID uuid.UUID) []string {
	ws := strings.ReplaceAll(workspaceID.String(), "-", "")
	return []string{string(c), "w" + ws[:12]}
}

type LinkStatus string

const (
	LinkPendente LinkStatus = "pendente" // sem credencial da Shopee
	LinkGerando  LinkStatus = "gerando"
	LinkPronto   LinkStatus = "pronto"
	LinkFalhou   LinkStatus = "falhou"
)

// Dono identifica a coleção: um usuário dentro de um workspace.
type Dono struct {
	WorkspaceID uuid.UUID
	UsuarioID   uuid.UUID
}

func (d Dono) escopo() postgres.Escopo {
	return postgres.Escopo{UsuarioID: d.UsuarioID.String(), WorkspaceID: d.WorkspaceID.String()}
}

type ProdutoResumo struct {
	ID                    uuid.UUID `json:"id"`
	Nome                  string    `json:"nome"`
	ImagemURL             *string   `json:"imagem_url"`
	LojaNome              string    `json:"loja_nome"`
	URL                   string    `json:"url"`
	PrecoMinCentavos      int64     `json:"preco_min_centavos"`
	PrecoMaxCentavos      int64     `json:"preco_max_centavos"`
	ComissaoBP            int32     `json:"comissao_bp"`
	GanhoPorVendaCentavos int64     `json:"ganho_por_venda_centavos"`
	Vendas                int64     `json:"vendas"`
	Nota                  *float64  `json:"nota"`
	AtualizadoEm          time.Time `json:"atualizado_em"`
}

// ResumoDe monta o resumo do produto que acompanha itens e listas.
func ResumoDe(p produtos.Produto) ProdutoResumo {
	return ProdutoResumo{
		ID: p.ID, Nome: p.Nome, ImagemURL: p.ImagemURL, LojaNome: p.LojaNome, URL: p.URL,
		PrecoMinCentavos: p.PrecoMinCentavos, PrecoMaxCentavos: p.PrecoMaxCentavos, ComissaoBP: p.ComissaoBP,
		GanhoPorVendaCentavos: p.GanhoPorVendaCentavos(), Vendas: p.Vendas, Nota: p.Nota, AtualizadoEm: p.ColetadoEm,
	}
}

type LinkCanal struct {
	Canal Canal  `json:"canal"`
	SubID string `json:"sub_id"`
	URL   string `json:"url"`
}

type Item struct {
	ID           uuid.UUID     `json:"id"`
	Produto      ProdutoResumo `json:"produto"`
	Titulo       string        `json:"titulo"`
	Descricao    string        `json:"descricao"`
	Notas        string        `json:"notas"`
	Tags         []string      `json:"tags"`
	Status       Status        `json:"status"`
	LinkAfiliado *string       `json:"link_afiliado"`
	LinkOrigem   string        `json:"link_origem"`
	LinkStatus   LinkStatus    `json:"link_status"`
	Links        []LinkCanal   `json:"links"`
	ColecaoIDs   []uuid.UUID   `json:"colecao_ids"`
	CriadoEm     time.Time     `json:"criado_em"`
	AtualizadoEm time.Time     `json:"atualizado_em"`
}

type Pagina struct {
	Itens     []Item `json:"itens"`
	Total     int64  `json:"total"`
	Pagina    int    `json:"pagina"`
	PorPagina int    `json:"por_pagina"`
}

type Colecao struct {
	ID       uuid.UUID `json:"id"`
	Nome     string    `json:"nome"`
	Itens    int64     `json:"itens"`
	CriadoEm time.Time `json:"criado_em"`
}

type Filtro struct {
	Busca     string
	Status    Status
	ColecaoID *uuid.UUID
	Tag       string
	Pagina    int
	PorPagina int
}

// Edicao são os campos de um PATCH; nil não muda. Com MudarLink, Link nil
// volta ao link automático.
type Edicao struct {
	Titulo    *string
	Descricao *string
	Notas     *string
	Tags      *[]string
	Status    *Status
	MudarLink bool
	Link      *string
}

const (
	PorPaginaPadrao = 30
	PorPaginaMax    = 100
	maxTitulo       = 200
	maxDescricao    = 2000
	maxNotas        = 5000
	maxTags         = 20
	maxTag          = 30
	maxLink         = 500
	maxBusca        = 100
	maxNomeColecao  = 60
	maxColecoesItem = 100
)

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrItemNaoEncontrado      = &Erro{http.StatusNotFound, "item_nao_encontrado", "Produto salvo não encontrado."}
	ErrColecaoNaoEncontrada   = &Erro{http.StatusNotFound, "colecao_nao_encontrada", "Coleção não encontrada."}
	ErrColecaoExistente       = &Erro{http.StatusConflict, "colecao_existente", "Você já tem uma coleção com esse nome."}
	ErrProdutoNaoEncontrado   = &Erro{http.StatusNotFound, "produto_nao_encontrado", "Não encontramos esse produto no programa de afiliados da Shopee."}
	ErrLinkInvalido           = &Erro{http.StatusUnprocessableEntity, "link_invalido", "Cole o link de um produto da Shopee, como https://shopee.com.br/Nome-do-produto-i.123.456."}
	ErrLinkCurto              = &Erro{http.StatusUnprocessableEntity, "link_curto", "Esse é um link curto. Abra-o no navegador e cole aqui o endereço completo da página do produto."}
	ErrSemCredencial          = &Erro{http.StatusConflict, "sem_credencial", "Conecte a sua conta de afiliado da Shopee para gerar os links."}
	ErrImportacaoIndisponivel = &Erro{http.StatusServiceUnavailable, "importacao_indisponivel", "Por enquanto só dá para salvar produtos do radar."}
	ErrShopeeLimite           = &Erro{http.StatusTooManyRequests, "shopee_limite", "A Shopee está limitando as chamadas agora. Tente de novo em alguns instantes."}
	ErrShopeeIndisponivel     = &Erro{http.StatusBadGateway, "shopee_indisponivel", "Não conseguimos falar com a Shopee agora. Tente de novo em instantes."}
)

func erroValidacao(msg string) *Erro {
	return &Erro{http.StatusUnprocessableEntity, "dados_invalidos", msg}
}

// Fila enfileira o job gerar_link (em produção, o River).
type Fila interface {
	Enfileirar(ctx context.Context, args ...GerarLinkArgs) error
}

type Service struct {
	pool      *pgxpool.Pool
	produtos  *produtos.Service
	catalogo  fontes.Catalogo // nil: não busca produtos fora do catálogo
	afiliador fontes.Afiliador
	fila      Fila
	log       *slog.Logger
	agora     func() time.Time
}

// NewService monta o serviço. catalogo pode ser nil (sem a credencial do app,
// só dá para salvar produtos que já estão no catálogo); fila pode ser nil no
// worker, que não enfileira.
func NewService(pool *pgxpool.Pool, p *produtos.Service, catalogo fontes.Catalogo, afiliador fontes.Afiliador, fila Fila, log *slog.Logger) *Service {
	return &Service{pool: pool, produtos: p, catalogo: catalogo, afiliador: afiliador, fila: fila, log: log, agora: time.Now}
}

func (s *Service) tx(ctx context.Context, d Dono, fn func(*colecoesdb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, d.escopo(), func(tx pgx.Tx) error { return fn(colecoesdb.New(tx)) })
}

// Salvar guarda um produto na coleção: pelo id do catálogo ou pelo link da
// Shopee. Se ele já estava salvo, devolve o item existente e criado=false.
func (s *Service) Salvar(ctx context.Context, d Dono, produtoID *uuid.UUID, link string) (Item, bool, error) {
	p, err := s.ResolverProduto(ctx, d, produtoID, link)
	if err != nil {
		return Item{}, false, err
	}

	status, err := s.statusInicial(ctx, d)
	if err != nil {
		return Item{}, false, err
	}
	var row colecoesdb.ItensColecao
	criado := true
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		row, err = q.CriarItem(ctx, colecoesdb.CriarItemParams{
			WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ProdutoID: p.ID,
			Titulo: cortar(p.Nome, maxTitulo), LinkStatus: colecoesdb.LinkStatus(status),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			criado = false
			row, err = q.ItemPorProduto(ctx, colecoesdb.ItemPorProdutoParams{
				WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ProdutoID: p.ID,
			})
		}
		return err
	})
	if err != nil {
		return Item{}, false, err
	}
	if criado && status == LinkGerando {
		row = s.enfileirar(ctx, d, row)
	}
	it, err := s.montarUm(ctx, d, row)
	return it, criado, err
}

// ResolverProduto acha o produto pelo id do catálogo ou pelo link da Shopee
// (exatamente um dos dois). A curadoria usa o mesmo caminho para montar listas.
func (s *Service) ResolverProduto(ctx context.Context, d Dono, produtoID *uuid.UUID, link string) (produtos.Produto, error) {
	link = strings.TrimSpace(link)
	if (produtoID == nil) == (link == "") {
		return produtos.Produto{}, erroValidacao("Informe o produto do radar ou cole o link da Shopee.")
	}
	if produtoID == nil {
		return s.produtoDoLink(ctx, d, link)
	}
	p, err := s.produtos.Produto(ctx, d.escopo(), *produtoID)
	if errors.Is(err, produtos.ErrProdutoNaoEncontrado) {
		err = ErrProdutoNaoEncontrado
	}
	return p, err
}

// Importado é um produto que entra na coleção vindo de uma lista da
// curadoria, com o comentário do mentor.
type Importado struct {
	ProdutoID  uuid.UUID
	Comentario string
}

// ResultadoImportacao diz quantos itens entraram e quantos já estavam salvos.
type ResultadoImportacao struct {
	Criados   int        `json:"criados"`
	JaSalvos  int        `json:"ja_salvos"`
	ColecaoID *uuid.UUID `json:"colecao_id"`
	// LinkStatus dos itens novos: gerando com a Shopee conectada, pendente sem.
	LinkStatus LinkStatus `json:"link_status"`
}

// Importar salva vários produtos de uma vez. Os novos levam o comentário do
// mentor nas notas e entram na fila do gerar_link com a credencial do próprio
// usuário. Com colecao, todos (novos e já salvos) vão para a coleção com esse
// nome, criada se ainda não existir.
func (s *Service) Importar(ctx context.Context, d Dono, itens []Importado, colecao string) (ResultadoImportacao, error) {
	var nome string
	if colecao != "" {
		var err error
		if nome, err = nomeColecao(cortar(colecao, maxNomeColecao)); err != nil {
			return ResultadoImportacao{}, err
		}
	}
	ids := make([]uuid.UUID, len(itens))
	for i, it := range itens {
		ids[i] = it.ProdutoID
	}
	prods, err := s.produtos.Varios(ctx, d.escopo(), ids)
	if err != nil {
		return ResultadoImportacao{}, err
	}
	status, err := s.statusInicial(ctx, d)
	if err != nil {
		return ResultadoImportacao{}, err
	}

	out := ResultadoImportacao{LinkStatus: status}
	var novos []uuid.UUID
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		out.Criados, out.JaSalvos, novos = 0, 0, nil
		var colecaoID uuid.UUID
		if nome != "" {
			id, err := q.ColecaoPorNome(ctx, colecoesdb.ColecaoPorNomeParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Nome: nome})
			if errors.Is(err, pgx.ErrNoRows) {
				id, err = q.CriarColecao(ctx, colecoesdb.CriarColecaoParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Nome: nome})
			}
			if err != nil {
				return err
			}
			colecaoID = id
			out.ColecaoID = &id
		}
		for _, it := range itens {
			p, ok := prods[it.ProdutoID]
			if !ok {
				return ErrProdutoNaoEncontrado
			}
			notas := strings.TrimSpace(it.Comentario)
			if notas != "" {
				notas = cortar("Dica do mentor: "+notas, maxNotas)
			}
			row, err := q.CriarItemImportado(ctx, colecoesdb.CriarItemImportadoParams{
				WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ProdutoID: p.ID,
				Titulo: cortar(p.Nome, maxTitulo), Notas: notas, LinkStatus: colecoesdb.LinkStatus(status),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				out.JaSalvos++
				row, err = q.ItemPorProduto(ctx, colecoesdb.ItemPorProdutoParams{
					WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ProdutoID: p.ID,
				})
			} else if err == nil {
				out.Criados++
				novos = append(novos, row.ID)
			}
			if err != nil {
				return err
			}
			if nome != "" {
				if err := q.AdicionarColecaoItem(ctx, colecoesdb.AdicionarColecaoItemParams{
					ColecaoID: colecaoID, ItemID: row.ID, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return ResultadoImportacao{}, err
	}
	if status == LinkGerando && len(novos) > 0 {
		args := make([]GerarLinkArgs, len(novos))
		for i, id := range novos {
			args[i] = GerarLinkArgs{ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID}
		}
		if err := s.fila.Enfileirar(ctx, args...); err != nil {
			s.log.ErrorContext(ctx, "não foi possível enfileirar gerar_link da importação", "itens", len(novos), "err", err)
			for _, id := range novos {
				if errMarcar := s.marcar(ctx, d, id, LinkFalhou); errMarcar != nil {
					return out, errors.Join(err, errMarcar)
				}
			}
			out.LinkStatus = LinkFalhou
		}
	}
	return out, nil
}

// ItensDosProdutos devolve os itens que o usuário já salvou desses produtos,
// por produto_id. A curadoria usa para mostrar o link do afiliado na lista.
func (s *Service) ItensDosProdutos(ctx context.Context, d Dono, produtoIDs []uuid.UUID) (map[uuid.UUID]Item, error) {
	var rows []colecoesdb.ItensColecao
	if err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		rows, err = q.ItensDosProdutos(ctx, colecoesdb.ItensDosProdutosParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ProdutoIds: produtoIDs})
		return err
	}); err != nil {
		return nil, err
	}
	itens, err := s.montar(ctx, d, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Item, len(itens))
	for _, it := range itens {
		out[it.Produto.ID] = it
	}
	return out, nil
}

// produtoDoLink acha o produto do link no catálogo ou, se ainda não foi
// coletado, busca na Shopee com a credencial do app e o importa.
func (s *Service) produtoDoLink(ctx context.Context, d Dono, link string) (produtos.Produto, error) {
	if utf8.RuneCountInString(link) > 2000 {
		return produtos.Produto{}, ErrLinkInvalido
	}
	_, itemID, err := shopee.LerLink(link)
	switch {
	case errors.Is(err, shopee.ErrLinkCurto):
		return produtos.Produto{}, ErrLinkCurto
	case err != nil:
		return produtos.Produto{}, ErrLinkInvalido
	}
	p, err := s.produtos.PorItem(ctx, d.escopo(), fontes.Shopee, itemID)
	if !errors.Is(err, produtos.ErrProdutoNaoEncontrado) {
		return p, err
	}
	if s.catalogo == nil {
		return produtos.Produto{}, ErrImportacaoIndisponivel
	}
	o, err := s.catalogo.OfertaPorItem(ctx, itemID)
	switch {
	case errors.Is(err, fontes.ErrNaoEncontrado):
		return produtos.Produto{}, ErrProdutoNaoEncontrado
	case errors.Is(err, fontes.ErrLimite):
		return produtos.Produto{}, ErrShopeeLimite
	case err != nil:
		s.log.WarnContext(ctx, "shopee falhou ao buscar produto colado", "item_id", itemID, "err", err)
		return produtos.Produto{}, ErrShopeeIndisponivel
	}
	return s.produtos.Importar(ctx, fontes.Shopee, o, s.agora())
}

// statusInicial do link automático: gerando com credencial, pendente sem.
func (s *Service) statusInicial(ctx context.Context, d Dono) (LinkStatus, error) {
	ok, err := s.afiliador.Conectado(ctx, d.UsuarioID)
	if err != nil {
		return "", err
	}
	if ok {
		return LinkGerando, nil
	}
	return LinkPendente, nil
}

// enfileirar agenda o gerar_link. Se a fila falhar, o item fica como falhou,
// para o usuário pedir de novo.
func (s *Service) enfileirar(ctx context.Context, d Dono, row colecoesdb.ItensColecao) colecoesdb.ItensColecao {
	err := s.fila.Enfileirar(ctx, GerarLinkArgs{ItemID: row.ID, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
	if err == nil {
		return row
	}
	s.log.ErrorContext(ctx, "não foi possível enfileirar gerar_link", "item_id", row.ID, "err", err)
	if errMarcar := s.marcar(ctx, d, row.ID, LinkFalhou); errMarcar != nil {
		s.log.ErrorContext(ctx, "não foi possível marcar o link como falhou", "item_id", row.ID, "err", errMarcar)
		return row
	}
	row.LinkStatus = colecoesdb.LinkStatusFalhou
	return row
}

func (s *Service) marcar(ctx context.Context, d Dono, id uuid.UUID, st LinkStatus) error {
	return s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		return q.MarcarLinkStatus(ctx, colecoesdb.MarcarLinkStatusParams{
			ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, LinkStatus: colecoesdb.LinkStatus(st),
		})
	})
}

// Listar devolve uma página dos itens do usuário, os mais novos primeiro.
func (s *Service) Listar(ctx context.Context, d Dono, f Filtro) (Pagina, error) {
	if f.Pagina == 0 {
		f.Pagina = 1
	}
	if f.PorPagina == 0 {
		f.PorPagina = PorPaginaPadrao
	}
	if f.Pagina < 1 || f.PorPagina < 1 || f.PorPagina > PorPaginaMax {
		return Pagina{}, erroValidacao("Paginação inválida.")
	}
	p := colecoesdb.ListarItensParams{
		WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, ColecaoID: f.ColecaoID,
		Limite: int32(f.PorPagina), Desloc: int32((f.Pagina - 1) * f.PorPagina),
	}
	if b := strings.TrimSpace(f.Busca); b != "" {
		if utf8.RuneCountInString(b) > maxBusca {
			return Pagina{}, erroValidacao("Busca muito longa.")
		}
		b = escaparLike(b)
		p.Busca = &b
	}
	if f.Status != "" {
		if !f.Status.valido() {
			return Pagina{}, erroValidacao("Status inválido.")
		}
		st := colecoesdb.ItemStatus(f.Status)
		p.Status = &st
	}
	if t := strings.TrimSpace(f.Tag); t != "" {
		p.Tag = &t
	}

	var rows []colecoesdb.ListarItensRow
	if err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		rows, err = q.ListarItens(ctx, p)
		return err
	}); err != nil {
		return Pagina{}, err
	}
	out := Pagina{Itens: []Item{}, Pagina: f.Pagina, PorPagina: f.PorPagina}
	itens := make([]colecoesdb.ItensColecao, len(rows))
	for i, r := range rows {
		out.Total = r.Total
		itens[i] = colecoesdb.ItensColecao{
			ID: r.ID, WorkspaceID: r.WorkspaceID, UsuarioID: r.UsuarioID, ProdutoID: r.ProdutoID,
			Titulo: r.Titulo, Descricao: r.Descricao, Notas: r.Notas, Tags: r.Tags, Status: r.Status,
			LinkAfiliado: r.LinkAfiliado, LinkOrigem: r.LinkOrigem, LinkStatus: r.LinkStatus,
			CriadoEm: r.CriadoEm, AtualizadoEm: r.AtualizadoEm,
		}
	}
	montados, err := s.montar(ctx, d, itens)
	if err != nil {
		return Pagina{}, err
	}
	out.Itens = montados
	return out, nil
}

// Ver devolve um item.
func (s *Service) Ver(ctx context.Context, d Dono, id uuid.UUID) (Item, error) {
	row, err := s.item(ctx, d, id)
	if err != nil {
		return Item{}, err
	}
	return s.montarUm(ctx, d, row)
}

func (s *Service) item(ctx context.Context, d Dono, id uuid.UUID) (colecoesdb.ItensColecao, error) {
	var row colecoesdb.ItensColecao
	err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		row, err = q.Item(ctx, colecoesdb.ItemParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrItemNaoEncontrado
	}
	return row, err
}

// Atualizar aplica a edição. Mudar para o link automático enfileira o
// gerar_link de novo.
func (s *Service) Atualizar(ctx context.Context, d Dono, id uuid.UUID, e Edicao) (Item, error) {
	p := colecoesdb.AtualizarItemParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID}
	var err error
	if p.Titulo, err = texto(e.Titulo, maxTitulo, "O título"); err != nil {
		return Item{}, err
	}
	if p.Descricao, err = texto(e.Descricao, maxDescricao, "A descrição"); err != nil {
		return Item{}, err
	}
	if p.Notas, err = texto(e.Notas, maxNotas, "As notas"); err != nil {
		return Item{}, err
	}
	if e.Tags != nil {
		if p.Tags, err = normalizarTags(*e.Tags); err != nil {
			return Item{}, err
		}
	}
	if e.Status != nil {
		if !e.Status.valido() {
			return Item{}, erroValidacao("Status inválido.")
		}
		st := colecoesdb.ItemStatus(*e.Status)
		p.Status = &st
	}
	var manual string
	var auto LinkStatus
	if e.MudarLink {
		if e.Link != nil {
			if manual, err = validarLink(*e.Link); err != nil {
				return Item{}, err
			}
		} else if auto, err = s.statusInicial(ctx, d); err != nil {
			return Item{}, err
		}
	}

	var row colecoesdb.ItensColecao
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		if row, err = q.AtualizarItem(ctx, p); err != nil {
			return err
		}
		switch {
		case !e.MudarLink:
		case e.Link != nil:
			row, err = q.DefinirLinkManual(ctx, colecoesdb.DefinirLinkManualParams{
				ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, LinkAfiliado: &manual,
			})
		default:
			row, err = prepararAuto(ctx, q, d, id, auto)
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrItemNaoEncontrado
	}
	if err != nil {
		return Item{}, err
	}
	if auto == LinkGerando {
		row = s.enfileirar(ctx, d, row)
	}
	return s.montarUm(ctx, d, row)
}

func prepararAuto(ctx context.Context, q *colecoesdb.Queries, d Dono, id uuid.UUID, st LinkStatus) (colecoesdb.ItensColecao, error) {
	if err := q.ApagarLinksCanal(ctx, colecoesdb.ApagarLinksCanalParams{ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID}); err != nil {
		return colecoesdb.ItensColecao{}, err
	}
	return q.PrepararLinkAuto(ctx, colecoesdb.PrepararLinkAutoParams{
		ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, LinkStatus: colecoesdb.LinkStatus(st),
	})
}

// GerarLink descarta o link atual (manual ou automático) e gera de novo.
func (s *Service) GerarLink(ctx context.Context, d Dono, id uuid.UUID) (Item, error) {
	return s.Atualizar(ctx, d, id, Edicao{MudarLink: true})
}

// GerarPendentes enfileira os links automáticos pendentes ou que falharam.
func (s *Service) GerarPendentes(ctx context.Context, d Dono) (int, error) {
	ok, err := s.afiliador.Conectado(ctx, d.UsuarioID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrSemCredencial
	}
	var ids []uuid.UUID
	if err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		ids, err = q.ItensSemLink(ctx, colecoesdb.ItensSemLinkParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		return err
	}); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]GerarLinkArgs, len(ids))
	for i, id := range ids {
		args[i] = GerarLinkArgs{ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID}
	}
	if err := s.fila.Enfileirar(ctx, args...); err != nil {
		for _, id := range ids {
			if errMarcar := s.marcar(ctx, d, id, LinkFalhou); errMarcar != nil {
				return 0, errors.Join(err, errMarcar)
			}
		}
		return 0, err
	}
	return len(ids), nil
}

// Remover apaga o item, os seus links e a sua presença nas coleções.
func (s *Service) Remover(ctx context.Context, d Dono, id uuid.UUID) error {
	return s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		n, err := q.RemoverItem(ctx, colecoesdb.RemoverItemParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		if err == nil && n == 0 {
			return ErrItemNaoEncontrado
		}
		return err
	})
}

// ProdutosSalvos lista os produto_id já salvos, para o radar marcar.
func (s *Service) ProdutosSalvos(ctx context.Context, d Dono) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		ids, err = q.ProdutosSalvos(ctx, colecoesdb.ProdutosSalvosParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		return err
	})
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

// DefinirColecoes substitui as coleções do item.
func (s *Service) DefinirColecoes(ctx context.Context, d Dono, id uuid.UUID, colecaoIDs []uuid.UUID) (Item, error) {
	ids := slices.Clone(colecaoIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	ids = slices.Compact(ids)
	if len(ids) > maxColecoesItem {
		return Item{}, erroValidacao("Coleções demais para um item.")
	}
	var row colecoesdb.ItensColecao
	err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		var err error
		row, err = q.Item(ctx, colecoesdb.ItemParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrItemNaoEncontrado
		}
		if err != nil {
			return err
		}
		n, err := q.ContarColecoes(ctx, colecoesdb.ContarColecoesParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Ids: ids})
		if err != nil {
			return err
		}
		if n != int64(len(ids)) {
			return ErrColecaoNaoEncontrada
		}
		if err := q.LimparColecoesDoItem(ctx, colecoesdb.LimparColecoesDoItemParams{ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID}); err != nil {
			return err
		}
		for _, c := range ids {
			if err := q.AdicionarColecaoItem(ctx, colecoesdb.AdicionarColecaoItemParams{
				ColecaoID: c, ItemID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Item{}, err
	}
	return s.montarUm(ctx, d, row)
}

// Colecoes lista as coleções do usuário, em ordem alfabética.
func (s *Service) Colecoes(ctx context.Context, d Dono) ([]Colecao, error) {
	out := []Colecao{}
	err := s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		rows, err := q.ListarColecoes(ctx, colecoesdb.ListarColecoesParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		for _, r := range rows {
			out = append(out, Colecao{ID: r.ID, Nome: r.Nome, Itens: r.Itens, CriadoEm: r.CriadoEm})
		}
		return err
	})
	return out, err
}

func (s *Service) colecao(ctx context.Context, q *colecoesdb.Queries, d Dono, id uuid.UUID) (Colecao, error) {
	r, err := q.Colecao(ctx, colecoesdb.ColecaoParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Colecao{}, ErrColecaoNaoEncontrada
	}
	return Colecao{ID: r.ID, Nome: r.Nome, Itens: r.Itens, CriadoEm: r.CriadoEm}, err
}

// CriarColecao cria uma coleção vazia.
func (s *Service) CriarColecao(ctx context.Context, d Dono, nome string) (Colecao, error) {
	nome, err := nomeColecao(nome)
	if err != nil {
		return Colecao{}, err
	}
	var c Colecao
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		id, err := q.CriarColecao(ctx, colecoesdb.CriarColecaoParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Nome: nome})
		if err != nil {
			return err
		}
		c, err = s.colecao(ctx, q, d, id)
		return err
	})
	return c, traduzirUnica(err)
}

// RenomearColecao troca o nome da coleção.
func (s *Service) RenomearColecao(ctx context.Context, d Dono, id uuid.UUID, nome string) (Colecao, error) {
	nome, err := nomeColecao(nome)
	if err != nil {
		return Colecao{}, err
	}
	var c Colecao
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		n, err := q.RenomearColecao(ctx, colecoesdb.RenomearColecaoParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Nome: nome})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrColecaoNaoEncontrada
		}
		c, err = s.colecao(ctx, q, d, id)
		return err
	})
	return c, traduzirUnica(err)
}

// ApagarColecao apaga a coleção; os itens continuam salvos.
func (s *Service) ApagarColecao(ctx context.Context, d Dono, id uuid.UUID) error {
	return s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		n, err := q.ApagarColecao(ctx, colecoesdb.ApagarColecaoParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		if err == nil && n == 0 {
			return ErrColecaoNaoEncontrada
		}
		return err
	})
}

func traduzirUnica(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrColecaoExistente
	}
	return err
}

func (s *Service) montarUm(ctx context.Context, d Dono, row colecoesdb.ItensColecao) (Item, error) {
	itens, err := s.montar(ctx, d, []colecoesdb.ItensColecao{row})
	if err != nil {
		return Item{}, err
	}
	return itens[0], nil
}

// montar junta a cada item o produto (pelo módulo produtos), os links por
// canal e as coleções.
func (s *Service) montar(ctx context.Context, d Dono, rows []colecoesdb.ItensColecao) ([]Item, error) {
	out := make([]Item, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	produtoIDs := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		produtoIDs[i] = r.ProdutoID
	}
	prods, err := s.produtos.Varios(ctx, d.escopo(), produtoIDs)
	if err != nil {
		return nil, err
	}
	links := map[uuid.UUID][]LinkCanal{}
	colecoes := map[uuid.UUID][]uuid.UUID{}
	err = s.tx(ctx, d, func(q *colecoesdb.Queries) error {
		ls, err := q.LinksDosItens(ctx, colecoesdb.LinksDosItensParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Ids: ids})
		if err != nil {
			return err
		}
		for _, l := range ls {
			links[l.ItemID] = append(links[l.ItemID], LinkCanal{Canal: Canal(l.Canal), SubID: l.SubID, URL: l.Url})
		}
		cs, err := q.ColecoesDosItens(ctx, colecoesdb.ColecoesDosItensParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Ids: ids})
		for _, c := range cs {
			colecoes[c.ItemID] = append(colecoes[c.ItemID], c.ColecaoID)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		p, ok := prods[r.ProdutoID]
		if !ok {
			return nil, fmt.Errorf("produto %s do item %s sumiu do catálogo", r.ProdutoID, r.ID)
		}
		it := Item{
			ID: r.ID, Titulo: r.Titulo, Descricao: r.Descricao, Notas: r.Notas, Tags: r.Tags,
			Status: Status(r.Status), LinkAfiliado: r.LinkAfiliado, LinkOrigem: string(r.LinkOrigem),
			LinkStatus: LinkStatus(r.LinkStatus), Links: links[r.ID], ColecaoIDs: colecoes[r.ID],
			CriadoEm: r.CriadoEm, AtualizadoEm: r.AtualizadoEm,
			Produto: ResumoDe(p),
		}
		if it.Tags == nil {
			it.Tags = []string{}
		}
		if it.Links == nil {
			it.Links = []LinkCanal{}
		}
		if it.ColecaoIDs == nil {
			it.ColecaoIDs = []uuid.UUID{}
		}
		out[i] = it
	}
	return out, nil
}

func texto(v *string, max int, campo string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t := strings.TrimSpace(*v)
	if utf8.RuneCountInString(t) > max {
		return nil, erroValidacao(fmt.Sprintf("%s pode ter até %d caracteres.", campo, max))
	}
	return &t, nil
}

// normalizarTags tira espaços, junta repetidas (sem diferenciar maiúsculas) e
// limita quantidade e tamanho.
func normalizarTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	vistas := map[string]bool{}
	for _, t := range tags {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > maxTag {
			return nil, erroValidacao(fmt.Sprintf("Cada tag pode ter até %d caracteres.", maxTag))
		}
		chave := strings.ToLower(t)
		if vistas[chave] {
			continue
		}
		vistas[chave] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, erroValidacao(fmt.Sprintf("Use no máximo %d tags.", maxTags))
	}
	return out, nil
}

func validarLink(v string) (string, error) {
	v = strings.TrimSpace(v)
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(v) > maxLink {
		return "", erroValidacao("O link de afiliado deve ser uma URL https, como https://s.shopee.com.br/abc123.")
	}
	return v, nil
}

func nomeColecao(nome string) (string, error) {
	nome = strings.Join(strings.Fields(nome), " ")
	if nome == "" || utf8.RuneCountInString(nome) > maxNomeColecao {
		return "", erroValidacao(fmt.Sprintf("O nome da coleção deve ter de 1 a %d caracteres.", maxNomeColecao))
	}
	return nome, nil
}

func cortar(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
