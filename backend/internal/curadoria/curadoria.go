// Package curadoria cuida das listas que o mentor monta para a turma: os
// produtos, um comentário por produto, a publicação (que notifica os
// membros) e a importação, em que o afiliado leva a lista, toda ou em parte,
// para a própria coleção e recebe os links com a credencial dele.
//
// Rascunhos só aparecem para dono e mentor. A coleção do afiliado é do módulo
// colecoes, e a curadoria a altera só por colecoes.Service.
package curadoria

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria/curadoriadb"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
)

type Lista struct {
	ID           uuid.UUID  `json:"id"`
	Titulo       string     `json:"titulo"`
	Descricao    string     `json:"descricao"`
	PublicadaEm  *time.Time `json:"publicada_em"`
	CriadoEm     time.Time  `json:"criado_em"`
	AtualizadoEm time.Time  `json:"atualizado_em"`
	Produtos     int64      `json:"produtos"`
	// Importadores só aparece para dono e mentor.
	Importadores *int64 `json:"importadores,omitempty"`
	// Importei diz se o usuário já importou algum produto da lista.
	Importei bool `json:"importei"`
}

// MeuItem é o produto já salvo na coleção de quem vê a lista, com o link dele.
type MeuItem struct {
	ID           uuid.UUID            `json:"id"`
	LinkAfiliado *string              `json:"link_afiliado"`
	LinkStatus   colecoes.LinkStatus  `json:"link_status"`
	Links        []colecoes.LinkCanal `json:"links"`
	Titulo       string               `json:"titulo"`
	Descricao    string               `json:"descricao"`
}

type ItemLista struct {
	Produto    colecoes.ProdutoResumo `json:"produto"`
	Comentario string                 `json:"comentario"`
	Ordem      int32                  `json:"ordem"`
	// Importadores só aparece para dono e mentor.
	Importadores *int64   `json:"importadores,omitempty"`
	MeuItem      *MeuItem `json:"meu_item"`
}

type ListaDetalhe struct {
	Lista
	Itens []ItemLista `json:"itens"`
}

type Importador struct {
	UsuarioID uuid.UUID `json:"usuario_id"`
	Nome      string    `json:"nome"`
	Produtos  int64     `json:"produtos"`
	UltimaEm  time.Time `json:"ultima_em"`
}

// Painel é o resumo da lista para o mentor: quem da turma importou.
type Painel struct {
	Afiliados    int          `json:"afiliados"`
	Importadores []Importador `json:"importadores"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrListaNaoEncontrada = &Erro{http.StatusNotFound, "lista_nao_encontrada", "Lista não encontrada."}
	ErrItemNaoEncontrado  = &Erro{http.StatusNotFound, "item_lista_nao_encontrado", "Esse produto não está na lista."}
	ErrSemPermissao       = &Erro{http.StatusForbidden, "sem_permissao", "Só o dono e os mentores podem montar listas."}
	ErrSoMentoria         = &Erro{http.StatusConflict, "so_mentoria", "Listas de curadoria são de workspaces de mentoria."}
	ErrLimiteListas       = &Erro{http.StatusConflict, "limite_listas", "O plano chegou ao limite de listas. Apague listas antigas para criar outras."}
	ErrListaVazia         = &Erro{http.StatusConflict, "lista_vazia", "Adicione ao menos um produto antes de publicar."}
	ErrJaNaLista          = &Erro{http.StatusConflict, "ja_na_lista", "Esse produto já está na lista."}
	ErrListaCheia         = &Erro{http.StatusConflict, "lista_cheia", fmt.Sprintf("Uma lista pode ter até %d produtos.", MaxItens)}
	ErrNaoPublicada       = &Erro{http.StatusConflict, "lista_nao_publicada", "Publique a lista antes de importar."}
	ErrOrdemInvalida      = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "Envie todos os produtos da lista, cada um uma vez, na nova ordem."}
	ErrProdutoForaDaLista = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "Algum dos produtos escolhidos não está na lista."}
)

const (
	// MaxItens é o máximo de produtos numa lista.
	MaxItens             = 100
	maxTitulo            = 120
	maxDescricao         = 2000
	maxComentario        = 1000
	limiteListasChave    = "listas"
	tipoNotificacaoLista = "lista_publicada"
)

func erroValidacao(msg string) *Erro {
	return &Erro{http.StatusUnprocessableEntity, "dados_invalidos", msg}
}

// Turma é o que a curadoria usa do módulo contas.
type Turma interface {
	Membros(ctx context.Context, m contas.Membro) ([]contas.MembroDetalhe, error)
	Workspace(ctx context.Context, m contas.Membro) (contas.Workspace, error)
	Limite(ctx context.Context, m contas.Membro, chave string) (int64, error)
}

// Notificador avisa a turma (implementado por notificacoes.Service).
type Notificador interface {
	Notificar(ctx context.Context, n notificacoes.Nova) error
}

type Service struct {
	pool        *pgxpool.Pool
	produtos    *produtos.Service
	colecoes    *colecoes.Service
	turma       Turma
	notificador Notificador
	log         *slog.Logger
}

func NewService(pool *pgxpool.Pool, p *produtos.Service, c *colecoes.Service, t Turma, n Notificador, log *slog.Logger) *Service {
	return &Service{pool: pool, produtos: p, colecoes: c, turma: t, notificador: n, log: log}
}

func escopo(m contas.Membro) postgres.Escopo {
	return postgres.Escopo{UsuarioID: m.UsuarioID.String(), WorkspaceID: m.WorkspaceID.String()}
}

func donoColecao(m contas.Membro) colecoes.Dono {
	return colecoes.Dono{WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID}
}

func (s *Service) tx(ctx context.Context, m contas.Membro, fn func(*curadoriadb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, escopo(m), func(tx pgx.Tx) error { return fn(curadoriadb.New(tx)) })
}

func exigirGestor(m contas.Membro) error {
	if m.TipoWorkspace != contas.TipoMentoria {
		return ErrSoMentoria
	}
	if !m.Papel.Gestor() {
		return ErrSemPermissao
	}
	return nil
}

// Listas devolve as listas do workspace: os rascunhos primeiro (só para dono
// e mentor), depois as publicadas, das mais novas para as mais antigas.
func (s *Service) Listas(ctx context.Context, m contas.Membro) ([]Lista, error) {
	gestor := m.Papel.Gestor()
	out := []Lista{}
	err := s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		rows, err := q.Listas(ctx, curadoriadb.ListasParams{WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID, Rascunhos: gestor})
		for _, r := range rows {
			out = append(out, listaDe(curadoriadb.ListaRow(r), gestor))
		}
		return err
	})
	return out, err
}

// Criar cria um rascunho vazio.
func (s *Service) Criar(ctx context.Context, m contas.Membro, titulo, descricao string) (ListaDetalhe, error) {
	if err := exigirGestor(m); err != nil {
		return ListaDetalhe{}, err
	}
	t, err := validarTitulo(titulo)
	if err != nil {
		return ListaDetalhe{}, err
	}
	d, err := texto(descricao, maxDescricao, "A descrição")
	if err != nil {
		return ListaDetalhe{}, err
	}
	limite, err := s.turma.Limite(ctx, m, limiteListasChave)
	if err != nil {
		return ListaDetalhe{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		n, err := q.ContarListas(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		if n >= limite {
			return ErrLimiteListas
		}
		id, err = q.CriarLista(ctx, curadoriadb.CriarListaParams{WorkspaceID: m.WorkspaceID, AutorID: m.UsuarioID, Titulo: t, Descricao: d})
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// Ver devolve a lista com os produtos. Para quem vê, cada produto já salvo
// na coleção vem com o link de afiliado dele.
func (s *Service) Ver(ctx context.Context, m contas.Membro, id uuid.UUID) (ListaDetalhe, error) {
	gestor := m.Papel.Gestor()
	var out ListaDetalhe
	var itens []curadoriadb.ItensDaListaRow
	porProduto := map[uuid.UUID]int64{}
	err := s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		r, err := q.Lista(ctx, curadoriadb.ListaParams{ID: id, WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID, Rascunhos: gestor})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrListaNaoEncontrada
		}
		if err != nil {
			return err
		}
		out.Lista = listaDe(r, gestor)
		if itens, err = q.ItensDaLista(ctx, curadoriadb.ItensDaListaParams{ListaID: id, WorkspaceID: m.WorkspaceID}); err != nil {
			return err
		}
		if gestor {
			cont, err := q.ImportacoesPorProduto(ctx, curadoriadb.ImportacoesPorProdutoParams{ListaID: id, WorkspaceID: m.WorkspaceID})
			if err != nil {
				return err
			}
			for _, c := range cont {
				porProduto[c.ProdutoID] = c.Importadores
			}
		}
		return nil
	})
	if err != nil {
		return ListaDetalhe{}, err
	}

	ids := make([]uuid.UUID, len(itens))
	for i, it := range itens {
		ids[i] = it.ProdutoID
	}
	prods, err := s.produtos.Varios(ctx, escopo(m), ids)
	if err != nil {
		return ListaDetalhe{}, err
	}
	meus, err := s.colecoes.ItensDosProdutos(ctx, donoColecao(m), ids)
	if err != nil {
		return ListaDetalhe{}, err
	}
	out.Itens = make([]ItemLista, 0, len(itens))
	for _, it := range itens {
		p, ok := prods[it.ProdutoID]
		if !ok {
			return ListaDetalhe{}, fmt.Errorf("produto %s da lista %s sumiu do catálogo", it.ProdutoID, id)
		}
		il := ItemLista{Produto: colecoes.ResumoDe(p), Comentario: it.Comentario, Ordem: it.Ordem}
		if gestor {
			n := porProduto[it.ProdutoID]
			il.Importadores = &n
		}
		if meu, ok := meus[it.ProdutoID]; ok {
			il.MeuItem = &MeuItem{
				ID: meu.ID, LinkAfiliado: meu.LinkAfiliado, LinkStatus: meu.LinkStatus, Links: meu.Links,
				Titulo: meu.Titulo, Descricao: meu.Descricao,
			}
		}
		out.Itens = append(out.Itens, il)
	}
	return out, nil
}

// Atualizar muda título e descrição (nil não muda).
func (s *Service) Atualizar(ctx context.Context, m contas.Membro, id uuid.UUID, titulo, descricao *string) (ListaDetalhe, error) {
	if err := exigirGestor(m); err != nil {
		return ListaDetalhe{}, err
	}
	p := curadoriadb.AtualizarListaParams{ID: id, WorkspaceID: m.WorkspaceID}
	if titulo != nil {
		t, err := validarTitulo(*titulo)
		if err != nil {
			return ListaDetalhe{}, err
		}
		p.Titulo = &t
	}
	if descricao != nil {
		d, err := texto(*descricao, maxDescricao, "A descrição")
		if err != nil {
			return ListaDetalhe{}, err
		}
		p.Descricao = &d
	}
	if err := s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		n, err := q.AtualizarLista(ctx, p)
		if err == nil && n == 0 {
			return ErrListaNaoEncontrada
		}
		return err
	}); err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// Apagar apaga a lista. Os itens que os afiliados importaram continuam na
// coleção deles.
func (s *Service) Apagar(ctx context.Context, m contas.Membro, id uuid.UUID) error {
	if err := exigirGestor(m); err != nil {
		return err
	}
	return s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		n, err := q.ApagarLista(ctx, curadoriadb.ApagarListaParams{ID: id, WorkspaceID: m.WorkspaceID})
		if err == nil && n == 0 {
			return ErrListaNaoEncontrada
		}
		return err
	})
}

// editar trava a lista e roda fn, conferindo que ela existe. Só dono e mentor.
func (s *Service) editar(ctx context.Context, m contas.Membro, id uuid.UUID, fn func(*curadoriadb.Queries) error) error {
	if err := exigirGestor(m); err != nil {
		return err
	}
	return s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		if _, err := q.TravarLista(ctx, curadoriadb.TravarListaParams{ID: id, WorkspaceID: m.WorkspaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrListaNaoEncontrada
		} else if err != nil {
			return err
		}
		if err := fn(q); err != nil {
			return err
		}
		return q.TocarLista(ctx, curadoriadb.TocarListaParams{ID: id, WorkspaceID: m.WorkspaceID})
	})
}

// AdicionarProduto põe um produto no fim da lista: do radar (produtoID) ou
// colado por link da Shopee, como na coleção.
func (s *Service) AdicionarProduto(ctx context.Context, m contas.Membro, id uuid.UUID, produtoID *uuid.UUID, link, comentario string) (ListaDetalhe, error) {
	if err := exigirGestor(m); err != nil {
		return ListaDetalhe{}, err
	}
	c, err := texto(comentario, maxComentario, "O comentário")
	if err != nil {
		return ListaDetalhe{}, err
	}
	p, err := s.colecoes.ResolverProduto(ctx, donoColecao(m), produtoID, link)
	if err != nil {
		return ListaDetalhe{}, err
	}
	err = s.editar(ctx, m, id, func(q *curadoriadb.Queries) error {
		itens, err := q.ItensDaLista(ctx, curadoriadb.ItensDaListaParams{ListaID: id, WorkspaceID: m.WorkspaceID})
		if err != nil {
			return err
		}
		if len(itens) >= MaxItens {
			return ErrListaCheia
		}
		n, err := q.AdicionarItemLista(ctx, curadoriadb.AdicionarItemListaParams{
			ListaID: id, WorkspaceID: m.WorkspaceID, ProdutoID: p.ID, Comentario: c,
		})
		if err == nil && n == 0 {
			return ErrJaNaLista
		}
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// Comentar troca o comentário do mentor sobre um produto da lista.
func (s *Service) Comentar(ctx context.Context, m contas.Membro, id, produtoID uuid.UUID, comentario string) (ListaDetalhe, error) {
	c, err := texto(comentario, maxComentario, "O comentário")
	if err != nil {
		return ListaDetalhe{}, err
	}
	err = s.editar(ctx, m, id, func(q *curadoriadb.Queries) error {
		n, err := q.ComentarItemLista(ctx, curadoriadb.ComentarItemListaParams{
			ListaID: id, WorkspaceID: m.WorkspaceID, ProdutoID: produtoID, Comentario: c,
		})
		if err == nil && n == 0 {
			return ErrItemNaoEncontrado
		}
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// RemoverProduto tira um produto da lista.
func (s *Service) RemoverProduto(ctx context.Context, m contas.Membro, id, produtoID uuid.UUID) (ListaDetalhe, error) {
	err := s.editar(ctx, m, id, func(q *curadoriadb.Queries) error {
		n, err := q.RemoverItemLista(ctx, curadoriadb.RemoverItemListaParams{ListaID: id, WorkspaceID: m.WorkspaceID, ProdutoID: produtoID})
		if err == nil && n == 0 {
			return ErrItemNaoEncontrado
		}
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// Ordenar recebe todos os produtos da lista na nova ordem.
func (s *Service) Ordenar(ctx context.Context, m contas.Membro, id uuid.UUID, produtoIDs []uuid.UUID) (ListaDetalhe, error) {
	err := s.editar(ctx, m, id, func(q *curadoriadb.Queries) error {
		itens, err := q.ItensDaLista(ctx, curadoriadb.ItensDaListaParams{ListaID: id, WorkspaceID: m.WorkspaceID})
		if err != nil {
			return err
		}
		if !mesmoConjunto(itens, produtoIDs) {
			return ErrOrdemInvalida
		}
		_, err = q.OrdenarItensLista(ctx, curadoriadb.OrdenarItensListaParams{ListaID: id, WorkspaceID: m.WorkspaceID, ProdutoIds: produtoIDs})
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	return s.Ver(ctx, m, id)
}

// Publicar mostra a lista para a turma e avisa cada membro (menos quem
// publicou) na caixa de entrada, por e-mail e por push. Publicar de novo não
// avisa de novo.
func (s *Service) Publicar(ctx context.Context, m contas.Membro, id uuid.UUID) (ListaDetalhe, error) {
	var publicou bool
	err := s.editar(ctx, m, id, func(q *curadoriadb.Queries) error {
		itens, err := q.ItensDaLista(ctx, curadoriadb.ItensDaListaParams{ListaID: id, WorkspaceID: m.WorkspaceID})
		if err != nil {
			return err
		}
		if len(itens) == 0 {
			return ErrListaVazia
		}
		n, err := q.PublicarLista(ctx, curadoriadb.PublicarListaParams{ID: id, WorkspaceID: m.WorkspaceID})
		publicou = n == 1
		return err
	})
	if err != nil {
		return ListaDetalhe{}, err
	}
	l, err := s.Ver(ctx, m, id)
	if err != nil {
		return ListaDetalhe{}, err
	}
	if publicou {
		// A lista já está publicada; se o aviso falhar, a turma ainda a vê no app.
		if err := s.avisar(ctx, m, l); err != nil {
			s.log.ErrorContext(ctx, "não foi possível avisar a turma da lista publicada", "lista_id", id, "err", err)
		}
	}
	return l, nil
}

func (s *Service) avisar(ctx context.Context, m contas.Membro, l ListaDetalhe) error {
	membros, err := s.turma.Membros(ctx, m)
	if err != nil {
		return err
	}
	ws, err := s.turma.Workspace(ctx, m)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for _, mb := range membros {
		if mb.UsuarioID != m.UsuarioID {
			ids = append(ids, mb.UsuarioID)
		}
	}
	corpo := fmt.Sprintf("%s publicou %s para você divulgar.", ws.Nome, plural(len(l.Itens), "produto", "produtos"))
	if d := strings.TrimSpace(l.Descricao); d != "" {
		corpo += " " + d
	}
	return s.notificador.Notificar(ctx, notificacoes.Nova{
		WorkspaceID: m.WorkspaceID, UsuarioIDs: ids, Tipo: tipoNotificacaoLista,
		Chave: "lista:" + l.ID.String(), Titulo: "Nova lista: " + l.Titulo, Corpo: corpo,
		URL: "/w/" + m.WorkspaceID.String() + "/listas/" + l.ID.String(),
	})
}

// Importar leva produtos de uma lista publicada para a coleção de quem
// importa (todos, se produtoIDs vier vazio). Com colecao, os itens também vão
// para uma coleção com o nome da lista. Os links saem com a credencial do
// próprio usuário.
func (s *Service) Importar(ctx context.Context, m contas.Membro, id uuid.UUID, produtoIDs []uuid.UUID, colecao bool) (colecoes.ResultadoImportacao, error) {
	l, err := s.Ver(ctx, m, id)
	if err != nil {
		return colecoes.ResultadoImportacao{}, err
	}
	if l.PublicadaEm == nil {
		return colecoes.ResultadoImportacao{}, ErrNaoPublicada
	}
	escolhidos := map[uuid.UUID]bool{}
	for _, p := range produtoIDs {
		escolhidos[p] = true
	}
	var itens []colecoes.Importado
	for _, it := range l.Itens {
		if len(escolhidos) == 0 || escolhidos[it.Produto.ID] {
			itens = append(itens, colecoes.Importado{ProdutoID: it.Produto.ID, Comentario: it.Comentario})
			delete(escolhidos, it.Produto.ID)
		}
	}
	if len(escolhidos) > 0 {
		return colecoes.ResultadoImportacao{}, ErrProdutoForaDaLista
	}
	if len(itens) == 0 {
		return colecoes.ResultadoImportacao{}, ErrListaVazia
	}
	nome := ""
	if colecao {
		nome = l.Titulo
	}
	res, err := s.colecoes.Importar(ctx, donoColecao(m), itens, nome)
	if err != nil {
		return res, err
	}
	ids := make([]uuid.UUID, len(itens))
	for i, it := range itens {
		ids[i] = it.ProdutoID
	}
	err = s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		return q.RegistrarImportacao(ctx, curadoriadb.RegistrarImportacaoParams{
			ListaID: id, WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID, ProdutoIds: ids,
		})
	})
	return res, err
}

// Painel mostra ao mentor quem da turma importou a lista.
func (s *Service) Painel(ctx context.Context, m contas.Membro, id uuid.UUID) (Painel, error) {
	if err := exigirGestor(m); err != nil {
		return Painel{}, err
	}
	var rows []curadoriadb.ImportadoresRow
	err := s.tx(ctx, m, func(q *curadoriadb.Queries) error {
		if _, err := q.Lista(ctx, curadoriadb.ListaParams{ID: id, WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID, Rascunhos: true}); errors.Is(err, pgx.ErrNoRows) {
			return ErrListaNaoEncontrada
		} else if err != nil {
			return err
		}
		var err error
		rows, err = q.Importadores(ctx, curadoriadb.ImportadoresParams{ListaID: id, WorkspaceID: m.WorkspaceID})
		return err
	})
	if err != nil {
		return Painel{}, err
	}
	membros, err := s.turma.Membros(ctx, m)
	if err != nil {
		return Painel{}, err
	}
	nomes := map[uuid.UUID]string{}
	out := Painel{Importadores: []Importador{}}
	for _, mb := range membros {
		nomes[mb.UsuarioID] = mb.Nome
		if mb.Papel == contas.PapelAfiliado {
			out.Afiliados++
		}
	}
	for _, r := range rows {
		nome, ok := nomes[r.UsuarioID]
		if !ok {
			nome = "Ex-membro"
		}
		out.Importadores = append(out.Importadores, Importador{UsuarioID: r.UsuarioID, Nome: nome, Produtos: r.Produtos, UltimaEm: r.UltimaEm})
	}
	return out, nil
}

func listaDe(r curadoriadb.ListaRow, gestor bool) Lista {
	l := Lista{
		ID: r.ID, Titulo: r.Titulo, Descricao: r.Descricao, PublicadaEm: r.PublicadaEm,
		CriadoEm: r.CriadoEm, AtualizadoEm: r.AtualizadoEm, Produtos: r.Produtos, Importei: r.Importei,
	}
	if gestor {
		n := r.Importadores
		l.Importadores = &n
	}
	return l
}

func mesmoConjunto(itens []curadoriadb.ItensDaListaRow, ids []uuid.UUID) bool {
	if len(itens) != len(ids) {
		return false
	}
	a := make([]string, len(ids))
	b := make([]string, len(itens))
	for i := range ids {
		a[i], b[i] = ids[i].String(), itens[i].ProdutoID.String()
	}
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b) // b não repete: produto_id é chave da lista
}

func validarTitulo(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" || utf8.RuneCountInString(t) > maxTitulo {
		return "", erroValidacao(fmt.Sprintf("O título deve ter de 1 a %d caracteres.", maxTitulo))
	}
	return t, nil
}

func texto(v string, max int, campo string) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max {
		return "", erroValidacao(fmt.Sprintf("%s pode ter até %d caracteres.", campo, max))
	}
	return v, nil
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return "1 " + um
	}
	return fmt.Sprintf("%d %s", n, varios)
}
