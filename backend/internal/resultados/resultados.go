// Package resultados sincroniza as conversões de cada afiliado (pedidos e
// comissão, pelo conversionReport da Shopee com a credencial dele) e monta os
// painéis: o do afiliado, com os próprios números, e o da turma, com os
// números agregados de quem consentiu, por lista e por produto.
//
// A conversão é do usuário e fica no workspace marcado no subId do link
// (colecoes.SubIDs); sem marca, no workspace pessoal. O mentor nunca vê
// resultados individuais: só somas dos membros que consentiram (LGPD).
package resultados

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // fuso de Brasília mesmo em imagens sem tzdata

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/resultados/resultadosdb"
)

const (
	// JanelaSync é quanto cada sincronização relê: o máximo do
	// conversionReport (90 dias), com folga. Relê tudo porque a situação dos
	// pedidos muda depois da compra (pendente, concluído, cancelado).
	JanelaSync = 89 * 24 * time.Hour
	// IntervaloMinimo entre duas sincronizações pedidas pelo usuário.
	IntervaloMinimo = 10 * time.Minute
	// PeriodoMax é o maior período de uma consulta ao painel.
	PeriodoMax = 366
	// PeriodoPadrao é o período, em dias, quando a consulta não informa um.
	PeriodoPadrao = 30
	maxProdutos   = 50
)

// Fuso dos dias no painel.
var Fuso = carregarFuso()

func carregarFuso() *time.Location {
	l, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(err)
	}
	return l
}

type StatusSync string

const (
	SyncNunca         StatusSync = "nunca"
	SyncSincronizando StatusSync = "sincronizando"
	SyncOK            StatusSync = "ok"
	SyncErro          StatusSync = "erro"
	SyncSemCredencial StatusSync = "sem_credencial"
)

// Sincronizacao é a situação da sincronização do usuário. AtualizadoEm é o
// fim da última que deu certo.
type Sincronizacao struct {
	Status       StatusSync `json:"status"`
	PedidaEm     *time.Time `json:"pedida_em"`
	AtualizadoEm *time.Time `json:"atualizado_em"`
	Conversoes   int32      `json:"conversoes"`
	Erro         *string    `json:"erro"`
}

type Periodo struct {
	De  string `json:"de"`  // AAAA-MM-DD, no fuso de Brasília
	Ate string `json:"ate"` // inclusive

	inicio, fim time.Time // [inicio, fim)
}

type Totais struct {
	Pedidos                  int64 `json:"pedidos"`
	Cancelados               int64 `json:"cancelados"`
	Itens                    int64 `json:"itens"`
	VendasCentavos           int64 `json:"vendas_centavos"`
	ComissaoEstimadaCentavos int64 `json:"comissao_estimada_centavos"`
	ComissaoValidadaCentavos int64 `json:"comissao_validada_centavos"`
}

type Dia struct {
	Dia                      string `json:"dia"`
	Pedidos                  int64  `json:"pedidos"`
	ComissaoEstimadaCentavos int64  `json:"comissao_estimada_centavos"`
	ComissaoValidadaCentavos int64  `json:"comissao_validada_centavos"`
}

type Produto struct {
	ItemID                   int64      `json:"item_id"`
	ProdutoID                *uuid.UUID `json:"produto_id"`
	Nome                     string     `json:"nome"`
	LojaNome                 string     `json:"loja_nome"`
	ImagemURL                *string    `json:"imagem_url"`
	Pedidos                  int64      `json:"pedidos"`
	Itens                    int64      `json:"itens"`
	VendasCentavos           int64      `json:"vendas_centavos"`
	ComissaoEstimadaCentavos int64      `json:"comissao_estimada_centavos"`
	ComissaoValidadaCentavos int64      `json:"comissao_validada_centavos"`
}

type Canal struct {
	// Canal do link; vazio quando a venda veio de um link criado fora do app.
	Canal                    string `json:"canal"`
	Pedidos                  int64  `json:"pedidos"`
	ComissaoEstimadaCentavos int64  `json:"comissao_estimada_centavos"`
	ComissaoValidadaCentavos int64  `json:"comissao_validada_centavos"`
}

// Meus são os resultados do próprio usuário no workspace.
type Meus struct {
	Periodo       Periodo       `json:"periodo"`
	Sincronizacao Sincronizacao `json:"sincronizacao"`
	// Consente só existe em mentorias: se o usuário mostra os resultados ao
	// mentor.
	Consente   *bool     `json:"consente"`
	Totais     Totais    `json:"totais"`
	PorDia     []Dia     `json:"por_dia"`
	PorProduto []Produto `json:"por_produto"`
	PorCanal   []Canal   `json:"por_canal"`
}

type Lista struct {
	ID                       uuid.UUID `json:"id"`
	Titulo                   string    `json:"titulo"`
	PublicadaEm              time.Time `json:"publicada_em"`
	Importadores             int       `json:"importadores"`
	Pedidos                  int64     `json:"pedidos"`
	VendasCentavos           int64     `json:"vendas_centavos"`
	ComissaoEstimadaCentavos int64     `json:"comissao_estimada_centavos"`
	ComissaoValidadaCentavos int64     `json:"comissao_validada_centavos"`
}

// Turma são os resultados agregados dos membros que consentiram.
type Turma struct {
	Periodo Periodo `json:"periodo"`
	// Afiliados é o total de afiliados; Consentem, quantos membros (afiliados
	// ou não) autorizam; Ativos, quantos dos que consentem venderam no período.
	Afiliados  int       `json:"afiliados"`
	Consentem  int       `json:"consentem"`
	Ativos     int64     `json:"ativos"`
	Totais     Totais    `json:"totais"`
	PorDia     []Dia     `json:"por_dia"`
	PorLista   []Lista   `json:"por_lista"`
	PorProduto []Produto `json:"por_produto"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrSemPermissao    = &Erro{http.StatusForbidden, "sem_permissao", "Só o dono e os mentores veem os resultados da turma."}
	ErrSoMentoria      = &Erro{http.StatusConflict, "so_mentoria", "Os resultados da turma são de workspaces de mentoria."}
	ErrSemCredencial   = &Erro{http.StatusConflict, "sem_credencial", "Conecte a sua conta de afiliado da Shopee para sincronizar os resultados."}
	ErrSyncRecente     = &Erro{http.StatusTooManyRequests, "sincronizado_agora", "Os resultados acabaram de ser atualizados. Tente de novo em alguns minutos."}
	ErrPeriodoInvalido = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", fmt.Sprintf("Informe o período com datas AAAA-MM-DD, de até %d dias.", PeriodoMax)}
)

// Contas é o que o módulo usa de contas.
type Contas interface {
	Workspaces(ctx context.Context, usuarioID uuid.UUID) ([]contas.Workspace, error)
	Membros(ctx context.Context, m contas.Membro) ([]contas.MembroDetalhe, error)
	DefinirConsentimento(ctx context.Context, m contas.Membro, consente bool) error
}

// Listas dá as listas publicadas e as importações (curadoria.Service).
type Listas interface {
	Importacoes(ctx context.Context, m contas.Membro) ([]curadoria.ListaImportada, error)
}

// Conexao diz se o usuário tem a Shopee conectada (fontes.Afiliador).
type Conexao interface {
	Conectado(ctx context.Context, usuarioID uuid.UUID) (bool, error)
}

// Fila enfileira a sincronização.
type Fila interface {
	Enfileirar(ctx context.Context, args SyncConversoesArgs) error
}

type Service struct {
	pool      *pgxpool.Pool
	relatorio fontes.Relatorio
	conexao   Conexao
	produtos  *produtos.Service
	contas    Contas
	listas    Listas
	fila      Fila
	log       *slog.Logger
	agora     func() time.Time
}

func NewService(pool *pgxpool.Pool, relatorio fontes.Relatorio, conexao Conexao, prods *produtos.Service,
	c Contas, l Listas, fila Fila, log *slog.Logger) *Service {
	return &Service{
		pool: pool, relatorio: relatorio, conexao: conexao, produtos: prods, contas: c, listas: l,
		fila: fila, log: log, agora: time.Now,
	}
}

// ComRelogio troca o relógio (testes).
func (s *Service) ComRelogio(agora func() time.Time) *Service {
	c := *s
	c.agora = agora
	return &c
}

func (s *Service) tx(ctx context.Context, e postgres.Escopo, fn func(*resultadosdb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, e, func(tx pgx.Tx) error { return fn(resultadosdb.New(tx)) })
}

func escopoUsuario(id uuid.UUID) postgres.Escopo { return postgres.Escopo{UsuarioID: id.String()} }

func escopoMembro(m contas.Membro) postgres.Escopo {
	return postgres.Escopo{UsuarioID: m.UsuarioID.String(), WorkspaceID: m.WorkspaceID.String()}
}

// NovoPeriodo interpreta de e ate (AAAA-MM-DD, inclusive). Vazios valem os
// últimos PeriodoPadrao dias até hoje.
func NovoPeriodo(de, ate string, agora time.Time) (Periodo, error) {
	hoje := agora.In(Fuso)
	fim := time.Date(hoje.Year(), hoje.Month(), hoje.Day(), 0, 0, 0, 0, Fuso)
	if ate != "" {
		t, err := time.ParseInLocation(time.DateOnly, ate, Fuso)
		if err != nil {
			return Periodo{}, ErrPeriodoInvalido
		}
		fim = t
	}
	inicio := fim.AddDate(0, 0, -(PeriodoPadrao - 1))
	if de != "" {
		t, err := time.ParseInLocation(time.DateOnly, de, Fuso)
		if err != nil {
			return Periodo{}, ErrPeriodoInvalido
		}
		inicio = t
	}
	if inicio.After(fim) || fim.Sub(inicio) >= PeriodoMax*24*time.Hour {
		return Periodo{}, ErrPeriodoInvalido
	}
	return Periodo{
		De: inicio.Format(time.DateOnly), Ate: fim.Format(time.DateOnly),
		inicio: inicio, fim: fim.AddDate(0, 0, 1),
	}, nil
}

// Meus devolve os resultados do usuário no workspace da requisição.
func (s *Service) Meus(ctx context.Context, m contas.Membro, p Periodo) (Meus, error) {
	out := Meus{Periodo: p}
	if m.TipoWorkspace == contas.TipoMentoria {
		c := m.ConsenteResultados
		out.Consente = &c
	}
	usuarios := []uuid.UUID{m.UsuarioID}
	var prods []resultadosdb.PorProdutoRow
	err := s.tx(ctx, escopoMembro(m), func(q *resultadosdb.Queries) error {
		sinc, err := q.Sincronizacao(ctx, m.UsuarioID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			out.Sincronizacao = Sincronizacao{Status: SyncNunca}
		case err != nil:
			return err
		default:
			out.Sincronizacao = sincronizacaoDe(sinc)
		}
		if out.Totais, out.PorDia, err = agregados(ctx, q, m.WorkspaceID, usuarios, p); err != nil {
			return err
		}
		if prods, err = q.PorProduto(ctx, resultadosdb.PorProdutoParams{
			WorkspaceID: m.WorkspaceID, Usuarios: usuarios, De: p.inicio, Ate: p.fim, Limite: maxProdutos,
		}); err != nil {
			return err
		}
		canais, err := q.PorCanal(ctx, resultadosdb.PorCanalParams{WorkspaceID: m.WorkspaceID, Usuarios: usuarios, De: p.inicio, Ate: p.fim})
		out.PorCanal = make([]Canal, 0, len(canais))
		for _, c := range canais {
			out.PorCanal = append(out.PorCanal, Canal{
				Canal: c.Canal, Pedidos: c.Pedidos,
				ComissaoEstimadaCentavos: c.ComissaoEstimadaCentavos, ComissaoValidadaCentavos: c.ComissaoValidadaCentavos,
			})
		}
		return err
	})
	if err != nil {
		return Meus{}, err
	}
	out.PorProduto, err = s.produtosDe(ctx, escopoMembro(m), prods)
	return out, err
}

// Turma devolve os resultados agregados dos membros que consentiram. Só dono
// e mentor, e só em mentorias.
func (s *Service) Turma(ctx context.Context, m contas.Membro, p Periodo) (Turma, error) {
	if m.TipoWorkspace != contas.TipoMentoria {
		return Turma{}, ErrSoMentoria
	}
	if !m.Papel.Gestor() {
		return Turma{}, ErrSemPermissao
	}
	membros, err := s.contas.Membros(ctx, m)
	if err != nil {
		return Turma{}, err
	}
	out := Turma{Periodo: p, PorLista: []Lista{}}
	consentem := map[uuid.UUID]bool{}
	usuarios := []uuid.UUID{}
	for _, mb := range membros {
		if mb.Papel == contas.PapelAfiliado {
			out.Afiliados++
		}
		if mb.ConsenteResultados {
			consentem[mb.UsuarioID] = true
			usuarios = append(usuarios, mb.UsuarioID)
		}
	}
	out.Consentem = len(usuarios)

	listas, err := s.listas.Importacoes(ctx, m)
	if err != nil {
		return Turma{}, err
	}
	var grupos []int32
	var impUsuarios, impProdutos []uuid.UUID
	var desde []time.Time
	for i, l := range listas {
		importadores := map[uuid.UUID]bool{}
		for _, imp := range l.Importacoes {
			importadores[imp.UsuarioID] = true
			if !consentem[imp.UsuarioID] {
				continue
			}
			grupos = append(grupos, int32(i))
			impUsuarios = append(impUsuarios, imp.UsuarioID)
			impProdutos = append(impProdutos, imp.ProdutoID)
			desde = append(desde, imp.ImportadoEm)
		}
		var publicada time.Time
		if l.PublicadaEm != nil {
			publicada = *l.PublicadaEm
		}
		out.PorLista = append(out.PorLista, Lista{ID: l.ID, Titulo: l.Titulo, PublicadaEm: publicada, Importadores: len(importadores)})
	}

	var prods []resultadosdb.PorProdutoRow
	err = s.tx(ctx, escopoMembro(m), func(q *resultadosdb.Queries) error {
		var err error
		if out.Totais, out.PorDia, err = agregados(ctx, q, m.WorkspaceID, usuarios, p); err != nil {
			return err
		}
		t, err := q.Totais(ctx, resultadosdb.TotaisParams{WorkspaceID: m.WorkspaceID, Usuarios: usuarios, De: p.inicio, Ate: p.fim})
		if err != nil {
			return err
		}
		out.Ativos = t.Afiliados
		if prods, err = q.PorProduto(ctx, resultadosdb.PorProdutoParams{
			WorkspaceID: m.WorkspaceID, Usuarios: usuarios, De: p.inicio, Ate: p.fim, Limite: maxProdutos,
		}); err != nil {
			return err
		}
		if len(grupos) == 0 {
			return nil
		}
		rows, err := q.PorGrupo(ctx, resultadosdb.PorGrupoParams{
			WorkspaceID: m.WorkspaceID, De: p.inicio, Ate: p.fim,
			Grupos: grupos, Usuarios: impUsuarios, Produtos: impProdutos, Desde: desde,
		})
		for _, r := range rows {
			l := &out.PorLista[r.Grupo]
			l.Pedidos, l.VendasCentavos = r.Pedidos, r.VendasCentavos
			l.ComissaoEstimadaCentavos, l.ComissaoValidadaCentavos = r.ComissaoEstimadaCentavos, r.ComissaoValidadaCentavos
		}
		return err
	})
	if err != nil {
		return Turma{}, err
	}
	out.PorProduto, err = s.produtosDe(ctx, escopoMembro(m), prods)
	return out, err
}

func agregados(ctx context.Context, q *resultadosdb.Queries, ws uuid.UUID, usuarios []uuid.UUID, p Periodo) (Totais, []Dia, error) {
	t, err := q.Totais(ctx, resultadosdb.TotaisParams{WorkspaceID: ws, Usuarios: usuarios, De: p.inicio, Ate: p.fim})
	if err != nil {
		return Totais{}, nil, err
	}
	totais := Totais{
		Pedidos: t.Pedidos, Cancelados: t.Cancelados, Itens: t.Itens, VendasCentavos: t.VendasCentavos,
		ComissaoEstimadaCentavos: t.ComissaoEstimadaCentavos, ComissaoValidadaCentavos: t.ComissaoValidadaCentavos,
	}
	rows, err := q.PorDia(ctx, resultadosdb.PorDiaParams{WorkspaceID: ws, Usuarios: usuarios, De: p.inicio, Ate: p.fim})
	if err != nil {
		return Totais{}, nil, err
	}
	dias := make([]Dia, 0, len(rows))
	for _, r := range rows {
		dias = append(dias, Dia{
			Dia: r.Dia.Time.Format(time.DateOnly), Pedidos: r.Pedidos,
			ComissaoEstimadaCentavos: r.ComissaoEstimadaCentavos, ComissaoValidadaCentavos: r.ComissaoValidadaCentavos,
		})
	}
	return totais, dias, nil
}

// produtosDe completa as linhas por produto com a foto do catálogo.
func (s *Service) produtosDe(ctx context.Context, e postgres.Escopo, rows []resultadosdb.PorProdutoRow) ([]Produto, error) {
	out := make([]Produto, 0, len(rows))
	var ids []uuid.UUID
	for _, r := range rows {
		p := Produto{
			ItemID: r.ItemID, Nome: r.ItemNome, LojaNome: r.LojaNome, Pedidos: r.Pedidos, Itens: r.Itens,
			VendasCentavos: r.VendasCentavos, ComissaoEstimadaCentavos: r.ComissaoEstimadaCentavos,
			ComissaoValidadaCentavos: r.ComissaoValidadaCentavos,
		}
		if txt, ok := r.ProdutoID.(string); ok {
			if id, err := uuid.Parse(txt); err == nil {
				p.ProdutoID = &id
				ids = append(ids, id)
			}
		}
		out = append(out, p)
	}
	cat, err := s.produtos.Varios(ctx, e, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].ProdutoID != nil {
			out[i].ImagemURL = cat[*out[i].ProdutoID].ImagemURL
		}
	}
	return out, nil
}

// DefinirConsentimento grava se o usuário mostra os resultados ao mentor.
func (s *Service) DefinirConsentimento(ctx context.Context, m contas.Membro, consente bool) error {
	return s.contas.DefinirConsentimento(ctx, m, consente)
}

// Sincronizacao devolve a situação da sincronização do usuário.
func (s *Service) Sincronizacao(ctx context.Context, usuarioID uuid.UUID) (Sincronizacao, error) {
	var out Sincronizacao
	err := s.tx(ctx, escopoUsuario(usuarioID), func(q *resultadosdb.Queries) error {
		r, err := q.Sincronizacao(ctx, usuarioID)
		if errors.Is(err, pgx.ErrNoRows) {
			out = Sincronizacao{Status: SyncNunca}
			return nil
		}
		out = sincronizacaoDe(r)
		return err
	})
	return out, err
}

// PedirSincronizacao enfileira a sincronização do usuário agora. Recusa se
// ele não tem a Shopee conectada ou se a última terminou há pouco.
func (s *Service) PedirSincronizacao(ctx context.Context, usuarioID uuid.UUID) (Sincronizacao, error) {
	ok, err := s.conexao.Conectado(ctx, usuarioID)
	if err != nil {
		return Sincronizacao{}, err
	}
	if !ok {
		return Sincronizacao{}, ErrSemCredencial
	}
	atual, err := s.Sincronizacao(ctx, usuarioID)
	if err != nil {
		return Sincronizacao{}, err
	}
	if atual.Status == SyncSincronizando && atual.PedidaEm != nil && s.agora().Sub(*atual.PedidaEm) < time.Hour {
		return atual, nil
	}
	if atual.Status == SyncOK && atual.AtualizadoEm != nil && s.agora().Sub(*atual.AtualizadoEm) < IntervaloMinimo {
		return Sincronizacao{}, ErrSyncRecente
	}
	var out Sincronizacao
	err = s.tx(ctx, escopoUsuario(usuarioID), func(q *resultadosdb.Queries) error {
		r, err := q.IniciarSincronizacao(ctx, resultadosdb.IniciarSincronizacaoParams{UsuarioID: usuarioID, Agora: s.agora()})
		out = sincronizacaoDe(r)
		return err
	})
	if err != nil {
		return Sincronizacao{}, err
	}
	if err := s.fila.Enfileirar(ctx, SyncConversoesArgs{UsuarioID: usuarioID}); err != nil {
		return Sincronizacao{}, err
	}
	return out, nil
}

// Sincronizar lê as conversões dos últimos JanelaSync e grava. Devolve
// quantas leu. Sem credencial, registra a situação e não é erro.
func (s *Service) Sincronizar(ctx context.Context, usuarioID uuid.UUID) (int, error) {
	fim := s.agora()
	convs, err := s.relatorio.Conversoes(ctx, usuarioID, fim.Add(-JanelaSync), fim)
	if errors.Is(err, fontes.ErrSemCredencial) {
		return 0, s.concluir(ctx, usuarioID, SyncSemCredencial, 0, nil)
	}
	if err != nil {
		return 0, err
	}
	ws, err := s.contas.Workspaces(ctx, usuarioID)
	if err != nil {
		return 0, err
	}
	porMarca := map[string]uuid.UUID{}
	var pessoal uuid.UUID
	for _, w := range ws {
		porMarca[colecoes.MarcaWorkspace(w.ID)] = w.ID
		if w.Tipo == contas.TipoPessoal {
			pessoal = w.ID
		}
	}
	if pessoal == uuid.Nil {
		return 0, errors.New("usuário sem workspace pessoal")
	}

	catalogo := map[int64]*uuid.UUID{}
	for _, c := range convs {
		if _, ok := catalogo[c.ItemID]; ok {
			continue
		}
		p, err := s.produtos.PorItem(ctx, escopoUsuario(usuarioID), fontes.Shopee, c.ItemID)
		switch {
		case errors.Is(err, produtos.ErrProdutoNaoEncontrado):
			catalogo[c.ItemID] = nil
		case err != nil:
			return 0, err
		default:
			catalogo[c.ItemID] = &p.ID
		}
	}

	err = s.tx(ctx, escopoUsuario(usuarioID), func(q *resultadosdb.Queries) error {
		for _, c := range convs {
			canal, marca := lerSubID(c.SubID)
			ws, ok := porMarca[marca]
			if !ok {
				ws = pessoal
			}
			var cn *resultadosdb.Canal
			if canal != "" {
				v := resultadosdb.Canal(string(canal))
				cn = &v
			}
			if err := q.SalvarConversao(ctx, resultadosdb.SalvarConversaoParams{
				UsuarioID: usuarioID, WorkspaceID: ws, Fonte: string(fontes.Shopee),
				ConversaoID: c.ConversaoID, PedidoID: c.PedidoID, ItemID: c.ItemID, ModeloID: c.ModeloID,
				ProdutoID: catalogo[c.ItemID], ItemNome: c.ItemNome, LojaNome: c.LojaNome, SubID: c.SubID,
				Canal: cn, Status: resultadosdb.PedidoStatus(c.Status), Quantidade: c.Quantidade,
				ValorCentavos: c.PrecoCentavos * int64(c.Quantidade), ComissaoCentavos: c.ComissaoCentavos,
				OcorridoEm: c.CompradoEm, ClicadoEm: c.ClicadoEm,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(convs), s.concluir(ctx, usuarioID, SyncOK, len(convs), nil)
}

func (s *Service) concluir(ctx context.Context, usuarioID uuid.UUID, st StatusSync, n int, erro *string) error {
	return s.tx(ctx, escopoUsuario(usuarioID), func(q *resultadosdb.Queries) error {
		return q.ConcluirSincronizacao(ctx, resultadosdb.ConcluirSincronizacaoParams{
			UsuarioID: usuarioID, Status: string(st), Agora: s.agora(), Conversoes: int32(min(n, 1<<31-1)), Erro: erro,
		})
	})
}

// lerSubID acha o canal e a marca do workspace nos subIds do link (unidos por
// hífen, como o relatório os devolve; subIds vazios aparecem como hífens
// seguidos).
func lerSubID(sub string) (canal colecoes.Canal, marca string) {
	for p := range strings.SplitSeq(sub, "-") {
		switch {
		case slices.Contains(colecoes.Canais, colecoes.Canal(p)) && canal == "":
			canal = colecoes.Canal(p)
		case colecoes.EhMarcaWorkspace(p) && marca == "":
			marca = p
		}
	}
	return canal, marca
}

func sincronizacaoDe(r resultadosdb.Sincronizacao) Sincronizacao {
	pedida := r.PedidaEm
	return Sincronizacao{
		Status: StatusSync(r.Status), PedidaEm: &pedida, AtualizadoEm: r.ConcluidaEm,
		Conversoes: r.Conversoes, Erro: r.Erro,
	}
}
