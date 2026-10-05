package resultados_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/midia"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres/pgtest"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/resultados"
)

const (
	appIDAna    = "18300001234"
	secretAna   = "s3gr3d0-da-ana"
	appIDBia    = "18300005678"
	secretBia   = "s3gr3d0-da-bia"
	noCatalogo  = 20
	diasDepois  = 30
	maxPaginaMk = 7 // página pequena no teste para exercitar o scrollId
)

type fila[T any] struct {
	mu   sync.Mutex
	jobs []T
}

func (f *fila[T]) Enfileirar(_ context.Context, args ...T) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, args...)
	return nil
}

func (f *fila[T]) tirar() []T {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.jobs
	f.jobs = nil
	return out
}

type filaSync struct {
	fila[resultados.SyncConversoesArgs]
}

func (f *filaSync) Enfileirar(ctx context.Context, a resultados.SyncConversoesArgs) error {
	return f.fila.Enfileirar(ctx, a)
}

type ambiente struct {
	t           *testing.T
	pool        *pgxpool.Pool
	router      http.Handler
	agora       time.Time
	mock        *shopee.Mock
	cliente     *shopee.Cliente
	credenciais *shopee.Credenciais
	produtos    *produtos.Service
	ofertas     []fontes.Oferta
	filaLinks   *fila[colecoes.GerarLinkArgs]
	filaSync    *filaSync
	gerarLink   *colecoes.GerarLinkWorker
	sync        *resultados.SyncConversoesWorker
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// O relógio dos resultados e da Shopee fica no futuro, para haver vendas
	// depois das importações feitas agora.
	agora := time.Now().Add(diasDepois * 24 * time.Hour)
	relogio := func() time.Time { return agora }

	chave := make([]byte, 32)
	_, _ = rand.Read(chave)
	kek, err := crypto.NovaKEKLocal("teste-1", base64.StdEncoding.EncodeToString(chave))
	if err != nil {
		t.Fatal(err)
	}
	mock := &shopee.Mock{Segredos: map[string]string{appIDAna: secretAna, appIDBia: secretBia}, Agora: relogio}
	cliente := shopee.NovoMock(mock, shopee.Config{})
	credenciais := shopee.NovasCredenciais(pool, crypto.NovoCofre(kek), cliente)
	app := shopee.CatalogoDoApp{Cliente: cliente, Credencial: shopee.Credencial{AppID: "1", Secret: "x"}}

	produtosSvc := produtos.NewService(pool)
	var ofertas []fontes.Oferta
	for pagina := 1; ; pagina++ {
		p, err := app.Ofertas(ctx, fontes.FiltroCatalogo{Pagina: pagina, Limite: 50})
		if err != nil {
			t.Fatal(err)
		}
		ofertas = append(ofertas, p.Ofertas...)
		if !p.TemProxima {
			break
		}
	}
	if err := produtosSvc.Registrar(ctx, fontes.Shopee, time.Now(), ofertas[:noCatalogo]); err != nil {
		t.Fatal(err)
	}

	afiliador := shopee.Afiliador{Credenciais: credenciais, Cliente: cliente}
	// Páginas pequenas, para exercitar o scrollId.
	relatorio := shopee.Relatorio{Credenciais: credenciais, Cliente: cliente, Limite: maxPaginaMk}
	filaLinks := &fila[colecoes.GerarLinkArgs]{}
	fs := &filaSync{}

	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	colecoesSvc := colecoes.NewService(pool, produtosSvc, app, afiliador, filaLinks, log)
	notificacoesSvc := notificacoes.NewService(pool, &fila[notificacoes.EntregarArgs]{}, contasSvc, nil, nil, "https://app.teste", log)
	midiaSvc := midia.NewService(pool, produtosSvc, &midia.OEmbed{}, nil, nil, contasSvc, nil, log)
	curadoriaSvc := curadoria.NewService(pool, produtosSvc, colecoesSvc, contasSvc, notificacoesSvc, midiaSvc, log)
	resultadosSvc := resultados.NewService(pool, relatorio, afiliador, produtosSvc, contasSvc, curadoriaSvc, fs, log).ComRelogio(relogio)

	r := httpserver.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{},
		colecoes.NewHandler(colecoesSvc, log).Modulo(),
		curadoria.NewHandler(curadoriaSvc, log).Modulo(),
		resultados.NewHandler(resultadosSvc, log).Modulo(),
	)

	return &ambiente{
		t: t, pool: pool, router: r, agora: agora, mock: mock, cliente: cliente, credenciais: credenciais,
		produtos: produtosSvc, ofertas: ofertas, filaLinks: filaLinks, filaSync: fs,
		gerarLink: &colecoes.GerarLinkWorker{Svc: colecoes.NewService(pool, produtosSvc, app, afiliador, nil, log), Log: log},
		sync: &resultados.SyncConversoesWorker{
			Svc: resultados.NewService(pool, relatorio, afiliador, produtosSvc, contasSvc, nil, nil, log).ComRelogio(relogio),
			Log: log,
		},
	}
}

func (a *ambiente) chamar(sub, metodo, caminho string, corpo any, out any) int {
	a.t.Helper()
	var body io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req := httptest.NewRequest(metodo, caminho, body)
	req.Header.Set("Authorization", "Bearer dev:"+sub)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: resposta inválida %q: %v", metodo, caminho, rec.Body.String(), err)
		}
	}
	return rec.Code
}

func (a *ambiente) exigir(sub, metodo, caminho string, corpo any, out any, status int) {
	a.t.Helper()
	var bruto json.RawMessage
	if out == nil {
		out = &bruto
	}
	if got := a.chamar(sub, metodo, caminho, corpo, out); got != status {
		a.t.Fatalf("%s %s como %q: status %d, quer %d (%+v)", metodo, caminho, sub, got, status, out)
	}
}

func (a *ambiente) exigirErro(sub, metodo, caminho string, corpo any, status int, codigo string) {
	a.t.Helper()
	var e httpserver.Erro
	if got := a.chamar(sub, metodo, caminho, corpo, &e); got != status || e.Codigo != codigo {
		a.t.Fatalf("%s %s como %q: %d %q, quer %d %q (%s)", metodo, caminho, sub, got, e.Codigo, status, codigo, e.Mensagem)
	}
}

func (a *ambiente) usuario(sub string) contas.Usuario {
	a.t.Helper()
	var u contas.Usuario
	a.exigir(sub, http.MethodGet, "/v1/eu", nil, &u, 200)
	return u
}

func (a *ambiente) pessoal(sub string) string {
	a.t.Helper()
	var ws []contas.Workspace
	a.exigir(sub, http.MethodGet, "/v1/workspaces", nil, &ws, 200)
	for _, w := range ws {
		if w.Tipo == contas.TipoPessoal {
			return w.ID.String()
		}
	}
	a.t.Fatal("sem workspace pessoal")
	return ""
}

func (a *ambiente) conectar(sub, appID, secret string) {
	a.t.Helper()
	if _, err := a.credenciais.Conectar(context.Background(), a.usuario(sub).ID, appID, secret); err != nil {
		a.t.Fatal(err)
	}
}

func (a *ambiente) produtoID(i int) uuid.UUID {
	a.t.Helper()
	p, err := a.produtos.PorItem(context.Background(), postgres.Escopo{}, fontes.Shopee, a.ofertas[i].ItemID)
	if err != nil {
		a.t.Fatal(err)
	}
	return p.ID
}

func (a *ambiente) mentoria(mestre string, afiliados ...string) string {
	a.t.Helper()
	var ws contas.Workspace
	a.exigir(mestre, http.MethodPost, "/v1/workspaces", map[string]any{"nome": "Turma do " + mestre}, &ws, 201)
	for _, sub := range afiliados {
		var c contas.Convite
		a.exigir(mestre, http.MethodPost, "/v1/workspaces/"+ws.ID.String()+"/convites", map[string]any{}, &c, 201)
		a.exigir(sub, http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, 200)
	}
	return ws.ID.String()
}

func (a *ambiente) gerarLinks() {
	a.t.Helper()
	for _, args := range a.filaLinks.tirar() {
		job := &river.Job[colecoes.GerarLinkArgs]{JobRow: &rivertype.JobRow{Kind: "gerar_link", Attempt: 1, MaxAttempts: 6}, Args: args}
		if err := a.gerarLink.Work(context.Background(), job); err != nil {
			a.t.Fatalf("gerar_link: %v", err)
		}
	}
}

func (a *ambiente) sincronizar(sub string) {
	a.t.Helper()
	job := &river.Job[resultados.SyncConversoesArgs]{
		JobRow: &rivertype.JobRow{Kind: "sync_conversoes", Attempt: 1, MaxAttempts: 5},
		Args:   resultados.SyncConversoesArgs{UsuarioID: a.usuario(sub).ID},
	}
	if err := a.sync.Work(context.Background(), job); err != nil {
		a.t.Fatalf("sync_conversoes: %v", err)
	}
}

// esperado soma o relatório da Shopee como o painel da Shopee somaria:
// pedidos não cancelados, comissão estimada (não cancelados) e validada
// (concluídos), filtrando as conversões por f.
func (a *ambiente) esperado(appID, secret string, f func(fontes.Conversao) bool) resultados.Totais {
	a.t.Helper()
	cred := shopee.Credencial{AppID: appID, Secret: secret}
	var convs []fontes.Conversao
	filtro := shopee.FiltroConversoes{De: a.agora.Add(-resultados.JanelaSync), Ate: a.agora, Limite: maxPaginaMk}
	for {
		p, err := a.cliente.Conversoes(context.Background(), cred, filtro)
		if err != nil {
			a.t.Fatal(err)
		}
		convs = append(convs, p.Conversoes...)
		if !p.TemProxima {
			break
		}
		filtro.ScrollID = p.ScrollID
	}
	var t resultados.Totais
	pedidos := map[string]bool{}
	cancelados := map[string]bool{}
	for _, c := range convs {
		if !f(c) {
			continue
		}
		if c.Status == fontes.PedidoCancelado {
			cancelados[c.PedidoID] = true
			continue
		}
		pedidos[c.PedidoID] = true
		t.Itens += int64(c.Quantidade)
		t.VendasCentavos += c.PrecoCentavos * int64(c.Quantidade)
		t.ComissaoEstimadaCentavos += c.ComissaoCentavos
		if c.Status == fontes.PedidoConcluido {
			t.ComissaoValidadaCentavos += c.ComissaoCentavos
		}
	}
	t.Pedidos, t.Cancelados = int64(len(pedidos)), int64(len(cancelados))
	return t
}

// periodoTodo cobre a janela inteira da sincronização.
func (a *ambiente) periodoTodo() string {
	ate := a.agora.In(resultados.Fuso)
	de := a.agora.Add(-resultados.JanelaSync).In(resultados.Fuso)
	return "?de=" + de.Format(time.DateOnly) + "&ate=" + ate.Format(time.DateOnly)
}

func temMarca(ws string) func(fontes.Conversao) bool {
	marca := colecoes.MarcaWorkspace(uuid.MustParse(ws))
	return func(c fontes.Conversao) bool { return strings.Contains(c.SubID, marca) }
}

// TestResultadosBatemComAShopee é o critério do M6: os números do painel
// batem com os do relatório da Shopee, separados por workspace, e o mentor só
// vê a soma de quem consentiu.
func TestResultadosBatemComAShopee(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mestre", "ana", "bia")
	base := "/v1/workspaces/" + ws
	pessoalAna := a.pessoal("ana")
	a.conectar("ana", appIDAna, secretAna)
	a.conectar("bia", appIDBia, secretBia)

	// O mentor publica uma lista com dois produtos; Ana importa. Ana também
	// salva um produto no workspace pessoal, e Bia um na mentoria.
	var l curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "Achados"}, &l, 201)
	lista := base + "/listas/" + l.ID.String()
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(0)}, nil, 201)
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(1)}, nil, 201)
	a.exigir("mestre", http.MethodPost, lista+"/publicar", nil, nil, 200)
	a.exigir("ana", http.MethodPost, lista+"/importar", map[string]any{}, nil, 200)
	a.exigir("ana", http.MethodPost, "/v1/workspaces/"+pessoalAna+"/itens", map[string]any{"produto_id": a.produtoID(2)}, nil, 201)
	a.exigir("bia", http.MethodPost, base+"/itens", map[string]any{"produto_id": a.produtoID(3)}, nil, 201)
	a.gerarLinks()

	// Antes de sincronizar: painel vazio, sincronização "nunca".
	var meus resultados.Meus
	a.exigir("ana", http.MethodGet, base+"/resultados", nil, &meus, 200)
	if meus.Sincronizacao.Status != resultados.SyncNunca || meus.Totais.Pedidos != 0 || meus.Consente == nil || *meus.Consente {
		t.Fatalf("antes da sincronização: %+v", meus)
	}

	a.sincronizar("ana")
	a.sincronizar("bia")
	a.sincronizar("ana") // de novo: não duplica

	todo := a.periodoTodo()
	quer := a.esperado(appIDAna, secretAna, temMarca(ws))
	a.exigir("ana", http.MethodGet, base+"/resultados"+todo, nil, &meus, 200)
	if quer.Pedidos == 0 || meus.Totais != quer {
		t.Fatalf("mentoria da Ana: %+v, quer %+v", meus.Totais, quer)
	}
	if meus.Sincronizacao.Status != resultados.SyncOK || meus.Sincronizacao.AtualizadoEm == nil {
		t.Fatalf("sincronização: %+v", meus.Sincronizacao)
	}
	var somaDias, somaCanais, somaProdutos int64
	for _, d := range meus.PorDia {
		somaDias += d.ComissaoEstimadaCentavos
	}
	for _, c := range meus.PorCanal {
		if c.Canal == "" {
			t.Fatalf("venda sem canal na mentoria: %+v", meus.PorCanal)
		}
		somaCanais += c.ComissaoEstimadaCentavos
	}
	for _, p := range meus.PorProduto {
		if p.ProdutoID == nil || p.ImagemURL == nil {
			t.Fatalf("produto sem catálogo: %+v", p)
		}
		somaProdutos += p.ComissaoEstimadaCentavos
	}
	if somaDias != quer.ComissaoEstimadaCentavos || somaCanais != quer.ComissaoEstimadaCentavos || somaProdutos != quer.ComissaoEstimadaCentavos {
		t.Fatalf("somas por dia %d, canal %d, produto %d; quer %d", somaDias, somaCanais, somaProdutos, quer.ComissaoEstimadaCentavos)
	}

	// No pessoal: tudo o que não tem a marca da mentoria (links do pessoal e
	// vendas por links de fora do app).
	var pessoal resultados.Meus
	a.exigir("ana", http.MethodGet, "/v1/workspaces/"+pessoalAna+"/resultados"+todo, nil, &pessoal, 200)
	querPessoal := a.esperado(appIDAna, secretAna, func(c fontes.Conversao) bool { return !temMarca(ws)(c) })
	if pessoal.Totais != querPessoal || pessoal.Consente != nil {
		t.Fatalf("pessoal da Ana: %+v, quer %+v", pessoal, querPessoal)
	}

	// Sem consentimento, o mentor vê zero; a Bia não vê a turma nem a Ana.
	var turma resultados.Turma
	a.exigir("mestre", http.MethodGet, base+"/resultados/turma"+todo, nil, &turma, 200)
	if turma.Afiliados != 2 || turma.Consentem != 0 || turma.Totais.Pedidos != 0 || len(turma.PorLista) != 1 || turma.PorLista[0].Pedidos != 0 {
		t.Fatalf("turma sem consentimento: %+v", turma)
	}
	a.exigirErro("bia", http.MethodGet, base+"/resultados/turma", nil, 403, "sem_permissao")
	a.exigirErro("mestre", http.MethodGet, "/v1/workspaces/"+a.pessoal("mestre")+"/resultados/turma", nil, 409, "so_mentoria")
	a.exigirErro("ana", http.MethodPut, "/v1/workspaces/"+pessoalAna+"/resultados/consentimento", map[string]any{"consente": true}, 409, "so_mentoria")
	var bia resultados.Meus
	a.exigir("bia", http.MethodGet, base+"/resultados"+todo, nil, &bia, 200)
	if bia.Totais != a.esperado(appIDBia, secretBia, temMarca(ws)) {
		t.Fatalf("Bia vê outros números: %+v", bia.Totais)
	}

	// Ana consente: o mentor passa a ver a soma dela, e a lista conta só as
	// vendas dos produtos importados, depois da importação.
	var c map[string]bool
	a.exigir("ana", http.MethodPut, base+"/resultados/consentimento", map[string]any{"consente": true}, &c, 200)
	if !c["consente"] {
		t.Fatalf("consentimento: %+v", c)
	}
	var membros []contas.MembroDetalhe
	a.exigir("mestre", http.MethodGet, base+"/membros", nil, &membros, 200)
	ana := a.usuario("ana").ID
	for _, m := range membros {
		if m.ConsenteResultados != (m.UsuarioID == ana) {
			t.Fatalf("membros: %+v", membros)
		}
	}
	a.exigir("mestre", http.MethodGet, base+"/resultados/turma"+todo, nil, &turma, 200)
	if turma.Consentem != 1 || turma.Totais != quer || turma.Ativos != 1 {
		t.Fatalf("turma com a Ana: %+v, quer %+v", turma, quer)
	}
	var importadoEm time.Time
	if err := a.pool.QueryRow(context.Background(), "SELECT min(importado_em) FROM importacoes").Scan(&importadoEm); err != nil {
		t.Fatal(err)
	}
	daLista := map[int64]bool{a.ofertas[0].ItemID: true, a.ofertas[1].ItemID: true}
	querLista := a.esperado(appIDAna, secretAna, func(c fontes.Conversao) bool {
		return temMarca(ws)(c) && daLista[c.ItemID] && !c.CompradoEm.Before(importadoEm)
	})
	pl := turma.PorLista[0]
	if pl.Importadores != 1 || pl.Pedidos == 0 || pl.Pedidos != querLista.Pedidos || pl.ComissaoEstimadaCentavos != querLista.ComissaoEstimadaCentavos {
		t.Fatalf("lista: %+v, quer %+v", pl, querLista)
	}

	// Bia consente também: soma das duas.
	a.exigir("bia", http.MethodPut, base+"/resultados/consentimento", map[string]any{"consente": true}, nil, 200)
	a.exigir("mestre", http.MethodGet, base+"/resultados/turma"+todo, nil, &turma, 200)
	if turma.Consentem != 2 || turma.Totais.ComissaoEstimadaCentavos != quer.ComissaoEstimadaCentavos+bia.Totais.ComissaoEstimadaCentavos {
		t.Fatalf("turma com as duas: %+v", turma.Totais)
	}

	// Ana revoga: some do painel na hora.
	a.exigir("ana", http.MethodPut, base+"/resultados/consentimento", map[string]any{"consente": false}, nil, 200)
	a.exigir("mestre", http.MethodGet, base+"/resultados/turma"+todo, nil, &turma, 200)
	if turma.Consentem != 1 || turma.Totais != bia.Totais {
		t.Fatalf("depois de revogar: %+v, quer %+v", turma.Totais, bia.Totais)
	}
}

// TestRLSConversoes confere a segunda barreira: mesmo uma query sem filtro só
// enxerga o que a política libera.
func TestRLSConversoes(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mestre", "ana")
	outra := a.mentoria("intruso", "bia")
	a.conectar("ana", appIDAna, secretAna)
	a.exigir("ana", http.MethodPost, "/v1/workspaces/"+ws+"/itens", map[string]any{"produto_id": a.produtoID(0)}, nil, 201)
	a.gerarLinks()
	a.sincronizar("ana")

	contar := func(sub, workspace string) int {
		t.Helper()
		e := postgres.Escopo{UsuarioID: a.usuario(sub).ID.String(), WorkspaceID: workspace}
		var n int
		err := postgres.InTx(context.Background(), a.pool, e, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM conversoes").Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	total := contar("ana", "")
	if total == 0 || contar("ana", ws) == 0 || contar("ana", ws) > total {
		t.Fatalf("Ana vê %d no total e %d na mentoria", total, contar("ana", ws))
	}
	if n := contar("mestre", ws); n != 0 {
		t.Fatalf("mentor vê %d conversões sem consentimento", n)
	}
	a.exigir("ana", http.MethodPut, "/v1/workspaces/"+ws+"/resultados/consentimento", map[string]any{"consente": true}, nil, 200)
	if n := contar("mestre", ws); n != contar("ana", ws) {
		t.Fatalf("mentor vê %d, Ana %d", n, contar("ana", ws))
	}
	// Outro workspace e outro usuário não veem nada, nem com consentimento.
	if n := contar("intruso", outra); n != 0 {
		t.Fatalf("outro mentor vê %d", n)
	}
	if n := contar("intruso", ws); n != 0 {
		t.Fatalf("não membro vê %d", n)
	}
	if n := contar("bia", ""); n != 0 {
		t.Fatalf("outro afiliado vê %d", n)
	}
	// Só a sincronização (sem workspace) grava.
	err := postgres.InTx(context.Background(), a.pool, postgres.Escopo{UsuarioID: a.usuario("ana").ID.String(), WorkspaceID: ws},
		func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "UPDATE conversoes SET comissao_centavos = 0")
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	var zerados int
	_ = a.pool.QueryRow(context.Background(), "SELECT count(*) FROM conversoes WHERE comissao_centavos = 0 AND status <> 'cancelado'").Scan(&zerados)
	if zerados != 0 {
		t.Fatalf("a API alterou %d conversões", zerados)
	}
}

func TestPedirSincronizacao(t *testing.T) {
	a := novoAmbiente(t)
	a.exigirErro("ana", http.MethodPost, "/v1/eu/resultados/sincronizar", nil, 409, "sem_credencial")
	a.conectar("ana", appIDAna, secretAna)

	var s resultados.Sincronizacao
	a.exigir("ana", http.MethodPost, "/v1/eu/resultados/sincronizar", nil, &s, 202)
	if s.Status != resultados.SyncSincronizando || len(a.filaSync.tirar()) != 1 {
		t.Fatalf("pedido: %+v", s)
	}
	// Pedir de novo enquanto roda não enfileira outro.
	a.exigir("ana", http.MethodPost, "/v1/eu/resultados/sincronizar", nil, &s, 202)
	if len(a.filaSync.tirar()) != 0 {
		t.Fatal("enfileirou de novo")
	}
	a.sincronizar("ana")
	a.exigir("ana", http.MethodGet, "/v1/eu/resultados/sincronizacao", nil, &s, 200)
	if s.Status != resultados.SyncOK || s.Conversoes == 0 {
		t.Fatalf("depois: %+v", s)
	}
	a.exigirErro("ana", http.MethodPost, "/v1/eu/resultados/sincronizar", nil, 429, "sincronizado_agora")

	// Credencial recusada (o Secret mudou na Shopee): a sincronização
	// registra o erro, sem detalhes da chamada, e a conexão fica inválida.
	a.conectar("bia", appIDBia, secretBia)
	a.mock.Segredos[appIDBia] = "trocado"
	a.sincronizar("bia")
	a.exigir("bia", http.MethodGet, "/v1/eu/resultados/sincronizacao", nil, &s, 200)
	if s.Status != resultados.SyncErro || s.Erro == nil || strings.Contains(*s.Erro, appIDBia) {
		t.Fatalf("credencial recusada: %+v", s)
	}
	if c, _ := a.credenciais.Ver(context.Background(), a.usuario("bia").ID); c.Status != shopee.StatusInvalido {
		t.Fatalf("conexão: %+v", c)
	}
	a.exigirErro("bia", http.MethodPost, "/v1/eu/resultados/sincronizar", nil, 409, "sem_credencial")
}

func TestPeriodo(t *testing.T) {
	agora := time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC) // 4/10 às 23h em Brasília
	p, err := resultados.NovoPeriodo("", "", agora)
	if err != nil || p.De != "2026-09-05" || p.Ate != "2026-10-04" {
		t.Fatalf("padrão: %+v %v", p, err)
	}
	for _, c := range [][2]string{{"2026-10-05", "2026-10-01"}, {"2025-01-01", "2026-10-01"}, {"ontem", ""}} {
		if _, err := resultados.NovoPeriodo(c[0], c[1], agora); err != resultados.ErrPeriodoInvalido {
			t.Fatalf("%v: %v", c, err)
		}
	}
}
