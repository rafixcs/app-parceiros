package colecoes_test

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
	"strconv"
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
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

const (
	appIDUsuario  = "18300001234"
	secretUsuario = "s3gr3d0-d0-usuario"
	// Quantos produtos do catálogo gravado entram no banco antes dos testes.
	// Os demais servem para testar o link colado de um produto fora do catálogo.
	noCatalogo = 10
)

// filaFalsa guarda os jobs enfileirados; processar roda o worker neles.
type filaFalsa struct {
	mu   sync.Mutex
	jobs []colecoes.GerarLinkArgs
}

func (f *filaFalsa) Enfileirar(_ context.Context, args ...colecoes.GerarLinkArgs) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, args...)
	return nil
}

func (f *filaFalsa) tirar() []colecoes.GerarLinkArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.jobs
	f.jobs = nil
	return out
}

type ambiente struct {
	t           *testing.T
	pool        *pgxpool.Pool
	router      http.Handler
	fila        *filaFalsa
	mock        *shopee.Mock
	credenciais *shopee.Credenciais
	worker      *colecoes.GerarLinkWorker
	produtos    *produtos.Service
	ofertas     []fontes.Oferta // catálogo gravado inteiro
}

func novoAmbiente(t *testing.T, comCatalogo bool) *ambiente {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	chave := make([]byte, 32)
	_, _ = rand.Read(chave)
	kek, err := crypto.NewLocalKEK("teste-1", base64.StdEncoding.EncodeToString(chave))
	if err != nil {
		t.Fatal(err)
	}
	mock := &shopee.Mock{Segredos: map[string]string{appIDUsuario: secretUsuario}}
	cliente := shopee.NovoMock(mock, shopee.Config{})
	credenciais := shopee.NovasCredenciais(pool, crypto.NewVault(kek), cliente)
	app := shopee.CatalogoDoApp{Cliente: cliente, Credencial: shopee.Credencial{AppID: "1", Secret: "x"}}

	// Catálogo: os primeiros produtos do mock entram no banco.
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

	var catalogo fontes.Catalogo
	if comCatalogo {
		catalogo = app
	}
	afiliador := shopee.Afiliador{Credenciais: credenciais, Cliente: cliente}
	fila := &filaFalsa{}
	svc := colecoes.NewService(pool, produtosSvc, catalogo, afiliador, fila, log)

	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(contas.NewService(pool, auth.Dev{}, "https://app.teste"), log).Rotas(r, auth.Dev{},
		colecoes.NewHandler(svc, log).Modulo())

	return &ambiente{
		t: t, pool: pool, router: r, fila: fila, mock: mock, credenciais: credenciais,
		worker:   &colecoes.GerarLinkWorker{Svc: colecoes.NewService(pool, produtosSvc, catalogo, afiliador, nil, log), Log: log},
		produtos: produtosSvc, ofertas: ofertas,
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
	var e httputil.ErrorBody
	if got := a.chamar(sub, metodo, caminho, corpo, &e); got != status || e.Code != codigo {
		a.t.Fatalf("%s %s como %q: %d %q, quer %d %q (%s)", metodo, caminho, sub, got, e.Code, status, codigo, e.Message)
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
	return ws[0].ID.String()
}

func (a *ambiente) conectar(sub string) {
	a.t.Helper()
	if _, err := a.credenciais.Conectar(context.Background(), a.usuario(sub).ID, appIDUsuario, secretUsuario); err != nil {
		a.t.Fatal(err)
	}
}

func (a *ambiente) produtoID(i int) uuid.UUID {
	a.t.Helper()
	p, err := a.produtos.PorItem(context.Background(), database.Scope{}, fontes.Shopee, a.ofertas[i].ItemID)
	if err != nil {
		a.t.Fatal(err)
	}
	return p.ID
}

// processar roda o worker em cada job enfileirado.
func (a *ambiente) processar() int {
	a.t.Helper()
	jobs := a.fila.tirar()
	for _, args := range jobs {
		job := &river.Job[colecoes.GerarLinkArgs]{
			JobRow: &rivertype.JobRow{Kind: "gerar_link", Attempt: 1, MaxAttempts: 6},
			Args:   args,
		}
		if err := a.worker.Work(context.Background(), job); err != nil {
			a.t.Fatalf("gerar_link: %v", err)
		}
	}
	return len(jobs)
}

func (a *ambiente) salvar(sub, ws string, corpo any) colecoes.Item {
	a.t.Helper()
	var it colecoes.Item
	a.exigir(sub, http.MethodPost, "/v1/workspaces/"+ws+"/itens", corpo, &it, http.StatusCreated)
	return it
}

func TestSalvarEGerarLink(t *testing.T) {
	a := novoAmbiente(t, true)
	ws := a.pessoal("ana")
	base := "/v1/workspaces/" + ws

	// Sem credencial: salva com o link pendente e não enfileira nada.
	it := a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(0)})
	if it.LinkStatus != colecoes.LinkPendente || it.LinkAfiliado != nil || it.LinkOrigem != "auto" {
		t.Fatalf("sem credencial: %+v", it)
	}
	if it.Titulo != a.ofertas[0].Nome || it.Status != colecoes.StatusTestando || it.Produto.Nome != a.ofertas[0].Nome {
		t.Fatalf("item novo: %+v", it)
	}
	if want := produtos.GanhoPorVenda(a.ofertas[0].PrecoMinCentavos, a.ofertas[0].ComissaoBP); it.Produto.GanhoPorVendaCentavos != want {
		t.Fatalf("ganho por venda %d, quer %d", it.Produto.GanhoPorVendaCentavos, want)
	}
	if n := a.processar(); n != 0 {
		t.Fatalf("enfileirou %d jobs sem credencial", n)
	}
	a.exigirErro("ana", http.MethodPost, base+"/itens/links-pendentes", nil, http.StatusConflict, "sem_credencial")

	// Salvar de novo devolve o mesmo item.
	var deNovo colecoes.Item
	a.exigir("ana", http.MethodPost, base+"/itens", map[string]any{"produto_id": a.produtoID(0)}, &deNovo, http.StatusOK)
	if deNovo.ID != it.ID {
		t.Fatalf("salvou duas vezes: %s e %s", it.ID, deNovo.ID)
	}

	// Ao conectar, os pendentes são gerados.
	a.conectar("ana")
	var r struct{ Enfileirados int }
	a.exigir("ana", http.MethodPost, base+"/itens/links-pendentes", nil, &r, http.StatusAccepted)
	if r.Enfileirados != 1 || a.processar() != 1 {
		t.Fatalf("pendentes enfileirados: %+v", r)
	}
	a.exigir("ana", http.MethodGet, base+"/itens/"+it.ID.String(), nil, &it, 200)
	if it.LinkStatus != colecoes.LinkPronto || it.LinkAfiliado == nil || len(it.Links) != len(colecoes.Canais) {
		t.Fatalf("depois de gerar: %+v", it)
	}
	urls := map[string]bool{}
	for _, l := range it.Links {
		if !strings.HasPrefix(l.URL, "https://s.shopee.com.br/") || !strings.HasPrefix(l.SubID, string(l.Canal)+"-w") {
			t.Fatalf("link do canal %s: %+v", l.Canal, l)
		}
		if l.Canal == colecoes.CanalOutro && l.URL != *it.LinkAfiliado {
			t.Fatalf("link principal %q, canal outro %q", *it.LinkAfiliado, l.URL)
		}
		urls[l.URL] = true
	}
	if len(urls) != len(colecoes.Canais) {
		t.Fatalf("canais com o mesmo link: %+v", it.Links)
	}

	// Com credencial, o item novo já nasce gerando.
	outro := a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(1)})
	if outro.LinkStatus != colecoes.LinkGerando || a.processar() != 1 {
		t.Fatalf("com credencial: %+v", outro)
	}

	var ids []uuid.UUID
	a.exigir("ana", http.MethodGet, base+"/itens/produtos", nil, &ids, 200)
	if len(ids) != 2 {
		t.Fatalf("produtos salvos: %v", ids)
	}

	// A Shopee passa a recusar a credencial: o link fica pendente e a
	// conexão, inválida.
	a.mock.Segredos[appIDUsuario] = "trocado"
	terceiro := a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(2)})
	a.processar()
	a.exigir("ana", http.MethodGet, base+"/itens/"+terceiro.ID.String(), nil, &terceiro, 200)
	if terceiro.LinkStatus != colecoes.LinkPendente {
		t.Fatalf("com credencial recusada: %+v", terceiro)
	}
	c, err := a.credenciais.Ver(context.Background(), a.usuario("ana").ID)
	if err != nil || c.Status != shopee.StatusInvalido {
		t.Fatalf("credencial: %+v %v", c, err)
	}

	a.exigir("ana", http.MethodDelete, base+"/itens/"+outro.ID.String(), nil, nil, http.StatusNoContent)
	a.exigirErro("ana", http.MethodGet, base+"/itens/"+outro.ID.String(), nil, 404, "item_nao_encontrado")
	a.exigirErro("ana", http.MethodDelete, base+"/itens/"+outro.ID.String(), nil, 404, "item_nao_encontrado")
	a.exigirErro("ana", http.MethodPost, base+"/itens", map[string]any{"produto_id": uuid.New()}, 404, "produto_nao_encontrado")
	a.exigirErro("ana", http.MethodPost, base+"/itens", map[string]any{}, 422, "dados_invalidos")
}

func TestColarLink(t *testing.T) {
	a := novoAmbiente(t, true)
	ws := a.pessoal("ana")
	base := "/v1/workspaces/" + ws
	link := func(o fontes.Oferta) string {
		return "https://shopee.com.br/Produto-Qualquer-i." + itoa(o.LojaID) + "." + itoa(o.ItemID) + "?sp_atk=abc"
	}

	// Produto já no catálogo.
	it := a.salvar("ana", ws, map[string]any{"url": link(a.ofertas[0])})
	if it.Produto.ID != a.produtoID(0) {
		t.Fatalf("link de produto do catálogo: %+v", it.Produto)
	}

	// Produto fora do catálogo: buscado na Shopee e importado.
	fora := a.ofertas[noCatalogo+5]
	it = a.salvar("ana", ws, map[string]any{"url": "shopee.com.br/product/" + itoa(fora.LojaID) + "/" + itoa(fora.ItemID)})
	if it.Produto.Nome != fora.Nome || it.Produto.ComissaoBP != fora.ComissaoBP {
		t.Fatalf("produto importado: %+v", it.Produto)
	}
	if _, err := a.produtos.PorItem(context.Background(), database.Scope{}, fontes.Shopee, fora.ItemID); err != nil {
		t.Fatalf("produto importado não está no catálogo: %v", err)
	}

	a.exigirErro("ana", http.MethodPost, base+"/itens", map[string]any{"url": "https://s.shopee.com.br/abc123"}, 422, "link_curto")
	a.exigirErro("ana", http.MethodPost, base+"/itens", map[string]any{"url": "https://exemplo.com/i.1.2"}, 422, "link_invalido")
	a.exigirErro("ana", http.MethodPost, base+"/itens", map[string]any{"url": "https://shopee.com.br/Nada-i.1.999"}, 404, "produto_nao_encontrado")
	a.exigirErro("ana", http.MethodPost, base+"/itens",
		map[string]any{"url": link(fora), "produto_id": a.produtoID(0)}, 422, "dados_invalidos")

	// Sem a credencial do app, só dá para colar link de produto do catálogo.
	b := novoAmbiente(t, false)
	wsB := b.pessoal("bia")
	b.salvar("bia", wsB, map[string]any{"url": link(b.ofertas[1])})
	b.exigirErro("bia", http.MethodPost, "/v1/workspaces/"+wsB+"/itens", map[string]any{"url": link(b.ofertas[noCatalogo+1])}, 503, "importacao_indisponivel")
}

func TestEditarItem(t *testing.T) {
	a := novoAmbiente(t, true)
	a.conectar("ana")
	ws := a.pessoal("ana")
	it := a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(0)})
	a.processar()
	caminho := "/v1/workspaces/" + ws + "/itens/" + it.ID.String()

	a.exigir("ana", http.MethodPatch, caminho, map[string]any{
		"titulo": "  Fone que vende muito  ", "descricao": "Bateria de 30 h", "notas": "Testar no reels",
		"tags": []string{"fone", " Fone ", "áudio  bom", ""}, "status": "campeao",
	}, &it, 200)
	if it.Titulo != "Fone que vende muito" || it.Descricao != "Bateria de 30 h" || it.Notas != "Testar no reels" ||
		it.Status != colecoes.StatusCampeao || strings.Join(it.Tags, "|") != "fone|áudio bom" {
		t.Fatalf("depois de editar: %+v", it)
	}
	// Campos ausentes não mudam; texto vazio apaga.
	a.exigir("ana", http.MethodPatch, caminho, map[string]any{"notas": ""}, &it, 200)
	if it.Notas != "" || it.Titulo != "Fone que vende muito" || len(it.Tags) != 2 {
		t.Fatalf("edição parcial: %+v", it)
	}

	a.exigirErro("ana", http.MethodPatch, caminho, map[string]any{"status": "vendido"}, 422, "dados_invalidos")
	a.exigirErro("ana", http.MethodPatch, caminho, map[string]any{"titulo": strings.Repeat("a", 201)}, 422, "dados_invalidos")
	a.exigirErro("ana", http.MethodPatch, caminho, map[string]any{"link_afiliado": "http://inseguro.com"}, 422, "dados_invalidos")
	a.exigirErro("ana", http.MethodPatch, caminho, map[string]any{"campo": 1}, 400, "json_invalido")

	// Link manual vence o automático, e o job não o sobrescreve.
	manual := "https://s.shopee.com.br/meulink"
	a.exigir("ana", http.MethodPatch, caminho, map[string]any{"link_afiliado": manual}, &it, 200)
	if it.LinkOrigem != "manual" || it.LinkAfiliado == nil || *it.LinkAfiliado != manual || it.LinkStatus != colecoes.LinkPronto {
		t.Fatalf("link manual: %+v", it)
	}
	_ = a.fila.Enfileirar(context.Background(), colecoes.GerarLinkArgs{ItemID: it.ID, WorkspaceID: uuid.MustParse(ws), UsuarioID: a.usuario("ana").ID})
	a.processar()
	a.exigir("ana", http.MethodGet, caminho, nil, &it, 200)
	if *it.LinkAfiliado != manual {
		t.Fatalf("o job sobrescreveu o link manual: %+v", it)
	}

	// null volta ao automático.
	a.exigir("ana", http.MethodPatch, caminho, map[string]any{"link_afiliado": nil}, &it, 200)
	if it.LinkOrigem != "auto" || it.LinkStatus != colecoes.LinkGerando || it.LinkAfiliado != nil || len(it.Links) != 0 {
		t.Fatalf("de volta ao automático: %+v", it)
	}
	if a.processar() != 1 {
		t.Fatal("não enfileirou o link automático")
	}
	a.exigir("ana", http.MethodPost, caminho+"/link", nil, &it, http.StatusAccepted)
	if it.LinkStatus != colecoes.LinkGerando || a.processar() != 1 {
		t.Fatalf("gerar de novo: %+v", it)
	}
	a.exigir("ana", http.MethodGet, caminho, nil, &it, 200)
	if it.LinkStatus != colecoes.LinkPronto || len(it.Links) != len(colecoes.Canais) {
		t.Fatalf("depois de gerar de novo: %+v", it)
	}
}

func TestColecoesEFiltros(t *testing.T) {
	a := novoAmbiente(t, true)
	ws := a.pessoal("ana")
	base := "/v1/workspaces/" + ws

	var achados, natal colecoes.Colecao
	a.exigir("ana", http.MethodPost, base+"/colecoes", map[string]any{"nome": " Achados  da semana "}, &achados, 201)
	if achados.Nome != "Achados da semana" || achados.Itens != 0 {
		t.Fatalf("coleção criada: %+v", achados)
	}
	a.exigirErro("ana", http.MethodPost, base+"/colecoes", map[string]any{"nome": "achados DA semana"}, 409, "colecao_existente")
	a.exigirErro("ana", http.MethodPost, base+"/colecoes", map[string]any{"nome": " "}, 422, "dados_invalidos")
	a.exigir("ana", http.MethodPost, base+"/colecoes", map[string]any{"nome": "Natal"}, &natal, 201)
	a.exigirErro("ana", http.MethodPatch, base+"/colecoes/"+natal.ID.String(), map[string]any{"nome": "Achados da semana"}, 409, "colecao_existente")
	a.exigir("ana", http.MethodPatch, base+"/colecoes/"+natal.ID.String(), map[string]any{"nome": "Natal 2026"}, &natal, 200)
	if natal.Nome != "Natal 2026" {
		t.Fatalf("renomear: %+v", natal)
	}

	var itens []colecoes.Item
	for i := range 4 {
		itens = append(itens, a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(i)}))
	}
	// Um item em duas coleções; outro só em uma.
	var it colecoes.Item
	a.exigir("ana", http.MethodPut, base+"/itens/"+itens[0].ID.String()+"/colecoes",
		map[string]any{"colecao_ids": []uuid.UUID{achados.ID, natal.ID, achados.ID}}, &it, 200)
	if len(it.ColecaoIDs) != 2 {
		t.Fatalf("coleções do item: %+v", it.ColecaoIDs)
	}
	a.exigir("ana", http.MethodPut, base+"/itens/"+itens[1].ID.String()+"/colecoes",
		map[string]any{"colecao_ids": []uuid.UUID{natal.ID}}, &it, 200)
	a.exigirErro("ana", http.MethodPut, base+"/itens/"+itens[1].ID.String()+"/colecoes",
		map[string]any{"colecao_ids": []uuid.UUID{uuid.New()}}, 404, "colecao_nao_encontrada")

	var cs []colecoes.Colecao
	a.exigir("ana", http.MethodGet, base+"/colecoes", nil, &cs, 200)
	if len(cs) != 2 || cs[0].Nome != "Achados da semana" || cs[0].Itens != 1 || cs[1].Itens != 2 {
		t.Fatalf("coleções: %+v", cs)
	}

	a.exigir("ana", http.MethodPatch, base+"/itens/"+itens[2].ID.String(),
		map[string]any{"titulo": "Garrafa térmica 100%_inox", "status": "descartado", "tags": []string{"Casa"}}, nil, 200)

	listar := func(query string) colecoes.Pagina {
		t.Helper()
		var p colecoes.Pagina
		a.exigir("ana", http.MethodGet, base+"/itens"+query, nil, &p, 200)
		return p
	}
	if p := listar(""); p.Total != 4 || len(p.Itens) != 4 || p.Itens[0].ID != itens[3].ID {
		t.Fatalf("todos, os mais novos primeiro: %d %+v", p.Total, p.Itens)
	}
	if p := listar("?colecao=" + natal.ID.String()); p.Total != 2 {
		t.Fatalf("filtro por coleção: %+v", p)
	}
	if p := listar("?status=descartado"); p.Total != 1 || p.Itens[0].ID != itens[2].ID {
		t.Fatalf("filtro por status: %+v", p)
	}
	if p := listar("?tag=Casa"); p.Total != 1 {
		t.Fatalf("filtro por tag: %+v", p)
	}
	if p := listar("?q=100%25_inox"); p.Total != 1 {
		t.Fatalf("busca com %% e _: %+v", p)
	}
	if p := listar("?q=%25"); p.Total != 1 {
		t.Fatalf("busca só por %%: %+v", p)
	}
	if p := listar("?por_pagina=3&pagina=2"); p.Total != 4 || len(p.Itens) != 1 {
		t.Fatalf("paginação: %+v", p)
	}
	a.exigirErro("ana", http.MethodGet, base+"/itens?status=x", nil, 422, "dados_invalidos")
	a.exigirErro("ana", http.MethodGet, base+"/itens?por_pagina=500", nil, 422, "dados_invalidos")

	// Apagar a coleção mantém os itens.
	a.exigir("ana", http.MethodDelete, base+"/colecoes/"+natal.ID.String(), nil, nil, http.StatusNoContent)
	a.exigir("ana", http.MethodGet, base+"/itens/"+itens[1].ID.String(), nil, &it, 200)
	if len(it.ColecaoIDs) != 0 {
		t.Fatalf("item ainda na coleção apagada: %+v", it.ColecaoIDs)
	}
	a.exigirErro("ana", http.MethodDelete, base+"/colecoes/"+natal.ID.String(), nil, 404, "colecao_nao_encontrada")
}

// TestVazamento confere que a coleção é só do usuário, dentro de um só
// workspace: nem o mentor, nem outro afiliado, nem o próprio usuário em
// outro workspace a enxergam, pela API ou direto no banco (RLS).
func TestVazamento(t *testing.T) {
	a := novoAmbiente(t, true)
	ctx := context.Background()

	var mentoria contas.Workspace
	a.exigir("mestre", http.MethodPost, "/v1/workspaces", map[string]any{"nome": "Turma"}, &mentoria, 201)
	for _, sub := range []string{"ana", "beto"} {
		var c contas.Convite
		a.exigir("mestre", http.MethodPost, "/v1/workspaces/"+mentoria.ID.String()+"/convites", map[string]any{}, &c, 201)
		a.exigir(sub, http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, 200)
	}
	ws := mentoria.ID.String()
	base := "/v1/workspaces/" + ws

	a.conectar("ana")
	it := a.salvar("ana", ws, map[string]any{"produto_id": a.produtoID(0)})
	a.processar()
	var col colecoes.Colecao
	a.exigir("ana", http.MethodPost, base+"/colecoes", map[string]any{"nome": "Minhas"}, &col, 201)
	a.exigir("ana", http.MethodPut, base+"/itens/"+it.ID.String()+"/colecoes", map[string]any{"colecao_ids": []uuid.UUID{col.ID}}, nil, 200)
	a.exigir("ana", http.MethodPatch, base+"/itens/"+it.ID.String(), map[string]any{"notas": "segredo"}, nil, 200)

	item := base + "/itens/" + it.ID.String()
	for _, sub := range []string{"mestre", "beto"} {
		var p colecoes.Pagina
		a.exigir(sub, http.MethodGet, base+"/itens", nil, &p, 200)
		var cs []colecoes.Colecao
		a.exigir(sub, http.MethodGet, base+"/colecoes", nil, &cs, 200)
		var ids []uuid.UUID
		a.exigir(sub, http.MethodGet, base+"/itens/produtos", nil, &ids, 200)
		if p.Total != 0 || len(cs) != 0 || len(ids) != 0 {
			t.Fatalf("%s vê a coleção da ana: %+v %+v %v", sub, p, cs, ids)
		}
		a.exigirErro(sub, http.MethodGet, item, nil, 404, "item_nao_encontrado")
		a.exigirErro(sub, http.MethodPatch, item, map[string]any{"titulo": "x"}, 404, "item_nao_encontrado")
		a.exigirErro(sub, http.MethodPost, item+"/link", nil, 404, "item_nao_encontrado")
		a.exigirErro(sub, http.MethodPut, item+"/colecoes", map[string]any{"colecao_ids": []uuid.UUID{}}, 404, "item_nao_encontrado")
		a.exigirErro(sub, http.MethodDelete, item, nil, 404, "item_nao_encontrado")
		a.exigirErro(sub, http.MethodPatch, base+"/colecoes/"+col.ID.String(), map[string]any{"nome": "x"}, 404, "colecao_nao_encontrada")
		a.exigirErro(sub, http.MethodDelete, base+"/colecoes/"+col.ID.String(), nil, 404, "colecao_nao_encontrada")
	}
	// A coleção da ana não aparece no workspace pessoal dela.
	pessoal := a.pessoal("ana")
	var p colecoes.Pagina
	a.exigir("ana", http.MethodGet, "/v1/workspaces/"+pessoal+"/itens", nil, &p, 200)
	if p.Total != 0 {
		t.Fatalf("item da mentoria aparece no workspace pessoal: %+v", p)
	}
	a.exigirErro("ana", http.MethodGet, "/v1/workspaces/"+pessoal+"/itens/"+it.ID.String(), nil, 404, "item_nao_encontrado")
	// Quem não é membro nem chega às rotas.
	a.exigirErro("intruso", http.MethodGet, base+"/itens", nil, 404, "workspace_nao_encontrado")

	// Direto no banco, com o papel da aplicação: as políticas escondem tudo de
	// outro usuário ou de outro workspace, e impedem gravar em nome da ana.
	contar := func(e database.Scope) int {
		t.Helper()
		total := 0
		err := database.InTx(ctx, a.pool, e, func(tx pgx.Tx) error {
			for _, tabela := range []string{"itens_colecao", "colecoes", "colecao_itens", "links_canal"} {
				var n int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+tabela).Scan(&n); err != nil {
					return err
				}
				total += n
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	ana := a.usuario("ana").ID.String()
	if n := contar(database.Scope{UserID: ana, WorkspaceID: ws}); n != 1+1+1+len(colecoes.Canais) {
		t.Fatalf("a ana vê %d linhas", n)
	}
	for nome, e := range map[string]database.Scope{
		"mentor":          {UserID: a.usuario("mestre").ID.String(), WorkspaceID: ws},
		"outro afiliado":  {UserID: a.usuario("beto").ID.String(), WorkspaceID: ws},
		"outro workspace": {UserID: ana, WorkspaceID: pessoal},
		"sem workspace":   {UserID: ana},
	} {
		if n := contar(e); n != 0 {
			t.Fatalf("%s vê %d linhas da ana", nome, n)
		}
	}
	err := database.InTx(ctx, a.pool, database.Scope{UserID: a.usuario("beto").ID.String(), WorkspaceID: ws}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO colecoes (workspace_id, usuario_id, nome) VALUES ($1, $2, 'invasão')", ws, ana)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("beto gravou uma coleção em nome da ana: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestFilaRiver enfileira no River de verdade: o mesmo item não entra duas
// vezes enquanto o job está na fila.
func TestFilaRiver(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := queue.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := queue.NewInsertClient(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	fila := colecoes.FilaRiver{Client: client}
	a := colecoes.GerarLinkArgs{ItemID: uuid.New(), WorkspaceID: uuid.New(), UsuarioID: uuid.New()}
	b := colecoes.GerarLinkArgs{ItemID: uuid.New(), WorkspaceID: a.WorkspaceID, UsuarioID: a.UsuarioID}
	if err := fila.Enfileirar(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if err := fila.Enfileirar(ctx, a); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'gerar_link' AND queue = 'shopee'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("%d jobs na fila, quer 2", n)
	}
}
