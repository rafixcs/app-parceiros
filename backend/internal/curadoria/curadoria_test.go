package curadoria_test

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
)

const (
	appIDUsuario  = "18300001234"
	secretUsuario = "s3gr3d0-d0-usuario"
	noCatalogo    = 10
	endpointPush  = "https://fcm.googleapis.com/fcm/send/abc123"
)

// fila guarda os jobs enfileirados de um tipo.
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

type remetente struct {
	mu     sync.Mutex
	emails []notificacoes.Email
}

func (r *remetente) Enviar(_ context.Context, e notificacoes.Email) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emails = append(r.emails, e)
	return nil
}

func (r *remetente) para(email string) []notificacoes.Email {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []notificacoes.Email
	for _, e := range r.emails {
		if e.Para == email {
			out = append(out, e)
		}
	}
	return out
}

type push struct {
	mu        sync.Mutex
	enviados  []notificacoes.Inscricao
	payloads  [][]byte
	expiradas map[string]bool
}

func (p *push) ChavePublica() string { return "chave-publica-teste" }

func (p *push) Enviar(_ context.Context, in notificacoes.Inscricao, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expiradas[in.Endpoint] {
		return notificacoes.ErrInscricaoExpirada
	}
	p.enviados = append(p.enviados, in)
	p.payloads = append(p.payloads, payload)
	return nil
}

type ambiente struct {
	t           *testing.T
	pool        *pgxpool.Pool
	router      http.Handler
	filaLinks   *fila[colecoes.GerarLinkArgs]
	filaAvisos  *fila[notificacoes.EntregarArgs]
	gerarLink   *colecoes.GerarLinkWorker
	notificacao *notificacoes.Service
	credenciais *shopee.Credenciais
	produtos    *produtos.Service
	ofertas     []fontes.Oferta
	remetente   *remetente
	push        *push
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	chave := make([]byte, 32)
	_, _ = rand.Read(chave)
	kek, err := crypto.NovaKEKLocal("teste-1", base64.StdEncoding.EncodeToString(chave))
	if err != nil {
		t.Fatal(err)
	}
	cliente := shopee.NovoMock(&shopee.Mock{Segredos: map[string]string{appIDUsuario: secretUsuario}}, shopee.Config{})
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
	filaLinks := &fila[colecoes.GerarLinkArgs]{}
	filaAvisos := &fila[notificacoes.EntregarArgs]{}
	rem := &remetente{}
	ps := &push{expiradas: map[string]bool{}}

	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	colecoesSvc := colecoes.NewService(pool, produtosSvc, app, afiliador, filaLinks, log)
	notificacoesSvc := notificacoes.NewService(pool, filaAvisos, contasSvc, rem, ps, "https://app.teste", log)
	contasSvc.EnviarConvitesCom(notificacoesSvc.EnviarConvite)
	midiaSvc := midia.NewService(pool, produtosSvc, &midia.OEmbed{}, nil, nil, contasSvc, nil, log)
	curadoriaSvc := curadoria.NewService(pool, produtosSvc, colecoesSvc, contasSvc, notificacoesSvc, midiaSvc, log)

	r := httpserver.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{},
		colecoes.NewHandler(colecoesSvc, log).Modulo(),
		curadoria.NewHandler(curadoriaSvc, log).Modulo(),
		notificacoes.NewHandler(notificacoesSvc, log).Modulo(),
	)

	return &ambiente{
		t: t, pool: pool, router: r, filaLinks: filaLinks, filaAvisos: filaAvisos,
		gerarLink:   &colecoes.GerarLinkWorker{Svc: colecoes.NewService(pool, produtosSvc, app, afiliador, nil, log), Log: log},
		notificacao: notificacoes.NewService(pool, nil, contas.NewService(pool, nil, "https://app.teste"), rem, ps, "https://app.teste", log),
		credenciais: credenciais, produtos: produtosSvc, ofertas: ofertas, remetente: rem, push: ps,
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

func (a *ambiente) conectar(sub string) {
	a.t.Helper()
	if _, err := a.credenciais.Conectar(context.Background(), a.usuario(sub).ID, appIDUsuario, secretUsuario); err != nil {
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

// mentoria cria a mentoria do mestre com os afiliados convidados.
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

func (a *ambiente) gerarLinks() int {
	a.t.Helper()
	jobs := a.filaLinks.tirar()
	for _, args := range jobs {
		job := &river.Job[colecoes.GerarLinkArgs]{JobRow: &rivertype.JobRow{Kind: "gerar_link", Attempt: 1, MaxAttempts: 6}, Args: args}
		if err := a.gerarLink.Work(context.Background(), job); err != nil {
			a.t.Fatalf("gerar_link: %v", err)
		}
	}
	return len(jobs)
}

func (a *ambiente) entregar() []notificacoes.EntregarArgs {
	a.t.Helper()
	jobs := a.filaAvisos.tirar()
	w := &notificacoes.EntregarWorker{Svc: a.notificacao}
	for _, args := range jobs {
		job := &river.Job[notificacoes.EntregarArgs]{JobRow: &rivertype.JobRow{Kind: "entregar_notificacao", Attempt: 1, MaxAttempts: 8}, Args: args}
		if err := w.Work(context.Background(), job); err != nil {
			a.t.Fatalf("entregar_notificacao: %v", err)
		}
	}
	return jobs
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestListaChegaAoAfiliado é o critério do M4: a lista do mentor chega ao
// afiliado, que a importa e recebe o link dele.
func TestListaChegaAoAfiliado(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mestre", "ana", "beto")
	base := "/v1/workspaces/" + ws
	a.conectar("ana")
	inscricao := map[string]any{"endpoint": endpointPush, "keys": map[string]string{"p256dh": "chave", "auth": "segredo"}}
	a.exigir("ana", http.MethodPost, "/v1/eu/push", inscricao, nil, http.StatusNoContent)

	// O mentor monta a lista: dois produtos do radar e um colado por link.
	var l curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "  Achados da   semana ", "descricao": "Para o fim de semana"}, &l, 201)
	if l.Titulo != "Achados da semana" || l.PublicadaEm != nil || len(l.Itens) != 0 {
		t.Fatalf("lista criada: %+v", l)
	}
	lista := base + "/listas/" + l.ID.String()
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(0), "comentario": "Vende muito no reels"}, &l, 201)
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(1)}, &l, 201)
	fora := a.ofertas[noCatalogo+2]
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"url": "https://shopee.com.br/x-i." + itoa(fora.LojaID) + "." + itoa(fora.ItemID)}, &l, 201)
	a.exigirErro("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(1)}, 409, "ja_na_lista")
	if len(l.Itens) != 3 || l.Itens[0].Comentario != "Vende muito no reels" || l.Itens[2].Produto.Nome != fora.Nome {
		t.Fatalf("itens: %+v", l.Itens)
	}

	// Reordena (o terceiro primeiro) e troca um comentário.
	ordem := []uuid.UUID{l.Itens[2].Produto.ID, l.Itens[0].Produto.ID, l.Itens[1].Produto.ID}
	a.exigir("mestre", http.MethodPut, lista+"/ordem", map[string]any{"produto_ids": ordem}, &l, 200)
	if l.Itens[0].Produto.ID != ordem[0] || l.Itens[2].Produto.ID != ordem[2] {
		t.Fatalf("ordem: %+v", l.Itens)
	}
	a.exigirErro("mestre", http.MethodPut, lista+"/ordem", map[string]any{"produto_ids": ordem[:2]}, 422, "dados_invalidos")
	a.exigirErro("mestre", http.MethodPut, lista+"/ordem", map[string]any{"produto_ids": []uuid.UUID{ordem[0], ordem[0], ordem[1]}}, 422, "dados_invalidos")
	a.exigir("mestre", http.MethodPatch, lista+"/itens/"+ordem[2].String(), map[string]any{"comentario": "Frete grátis"}, &l, 200)
	if l.Itens[2].Comentario != "Frete grátis" {
		t.Fatalf("comentário: %+v", l.Itens[2])
	}

	// Rascunho: os afiliados ainda não veem.
	var ls []curadoria.Lista
	a.exigir("ana", http.MethodGet, base+"/listas", nil, &ls, 200)
	if len(ls) != 0 {
		t.Fatalf("a ana vê o rascunho: %+v", ls)
	}
	a.exigirErro("ana", http.MethodGet, lista, nil, 404, "lista_nao_encontrada")
	a.exigirErro("ana", http.MethodPost, lista+"/importar", map[string]any{}, 404, "lista_nao_encontrada")

	// Publicar avisa a ana e o beto, não o mestre.
	a.exigir("mestre", http.MethodPost, lista+"/publicar", nil, &l, 200)
	if l.PublicadaEm == nil {
		t.Fatalf("não publicou: %+v", l.Lista)
	}
	avisos := a.entregar()
	if len(avisos) != 2 {
		t.Fatalf("avisos: %+v", avisos)
	}
	for _, sub := range []string{"ana", "beto"} {
		var c notificacoes.Caixa
		a.exigir(sub, http.MethodGet, base+"/notificacoes", nil, &c, 200)
		if c.NaoLidas != 1 || c.Notificacoes[0].Titulo != "Nova lista: Achados da semana" ||
			c.Notificacoes[0].URL != "/w/"+ws+"/listas/"+l.ID.String() || !strings.Contains(c.Notificacoes[0].Corpo, "3 produtos") {
			t.Fatalf("caixa de %s: %+v", sub, c)
		}
		emails := a.remetente.para(sub + "@dev.local")
		if len(emails) != 1 || !strings.Contains(emails[0].Texto, "https://app.teste/w/"+ws+"/listas/"+l.ID.String()) {
			t.Fatalf("e-mail de %s: %+v", sub, emails)
		}
	}
	if len(a.remetente.para("mestre@dev.local")) != 0 {
		t.Fatal("o mentor recebeu o aviso da própria lista")
	}
	if len(a.push.enviados) != 1 || a.push.enviados[0].Endpoint != endpointPush {
		t.Fatalf("push: %+v", a.push.enviados)
	}
	var msg map[string]string
	if err := json.Unmarshal(a.push.payloads[0], &msg); err != nil || msg["titulo"] != "Nova lista: Achados da semana" {
		t.Fatalf("payload do push: %s", a.push.payloads[0])
	}

	// Entregar de novo (retry) não repete e-mail nem push; publicar de novo não avisa.
	for _, av := range avisos {
		_ = a.filaAvisos.Enfileirar(context.Background(), av)
	}
	a.entregar()
	if len(a.remetente.para("ana@dev.local")) != 1 || len(a.push.enviados) != 1 {
		t.Fatal("a entrega repetida reenviou")
	}
	a.exigir("mestre", http.MethodPost, lista+"/publicar", nil, nil, 200)
	if n := len(a.entregar()); n != 0 {
		t.Fatalf("republicar avisou %d", n)
	}

	// A ana importa dois produtos: entram na coleção com a dica do mentor e
	// numa coleção com o nome da lista, e o link sai com a credencial dela.
	a.exigir("ana", http.MethodGet, base+"/listas", nil, &ls, 200)
	if len(ls) != 1 || ls[0].Importadores != nil || ls[0].Importei || ls[0].Produtos != 3 {
		t.Fatalf("listas da ana: %+v", ls)
	}
	var res colecoes.ResultadoImportacao
	a.exigir("ana", http.MethodPost, lista+"/importar", map[string]any{"produto_ids": []uuid.UUID{ordem[1], ordem[2]}}, &res, 200)
	if res.Criados != 2 || res.JaSalvos != 0 || res.ColecaoID == nil || res.LinkStatus != colecoes.LinkGerando {
		t.Fatalf("importação: %+v", res)
	}
	if a.gerarLinks() != 2 {
		t.Fatal("não gerou os links da importação")
	}
	var lv curadoria.ListaDetalhe
	a.exigir("ana", http.MethodGet, lista, nil, &lv, 200)
	if !lv.Importei || lv.Itens[0].MeuItem != nil || lv.Itens[0].Importadores != nil {
		t.Fatalf("lista vista pela ana: %+v", lv)
	}
	for _, it := range lv.Itens[1:] {
		if it.MeuItem == nil || it.MeuItem.LinkStatus != colecoes.LinkPronto || it.MeuItem.LinkAfiliado == nil ||
			!strings.HasPrefix(*it.MeuItem.LinkAfiliado, "https://s.shopee.com.br/") || len(it.MeuItem.Links) != len(colecoes.Canais) {
			t.Fatalf("link da ana no produto %s: %+v", it.Produto.Nome, it.MeuItem)
		}
	}
	var item colecoes.Item
	a.exigir("ana", http.MethodGet, base+"/itens/"+lv.Itens[1].MeuItem.ID.String(), nil, &item, 200)
	if item.Notas != "Dica do mentor: Vende muito no reels" || len(item.ColecaoIDs) != 1 || item.ColecaoIDs[0] != *res.ColecaoID {
		t.Fatalf("item importado: %+v", item)
	}
	var cs []colecoes.Colecao
	a.exigir("ana", http.MethodGet, base+"/colecoes", nil, &cs, 200)
	if len(cs) != 1 || cs[0].Nome != "Achados da semana" || cs[0].Itens != 2 {
		t.Fatalf("coleções da ana: %+v", cs)
	}

	// Importar tudo depois: só o que falta é novo, e a coleção é a mesma.
	a.exigir("ana", http.MethodPost, lista+"/importar", map[string]any{}, &res, 200)
	if res.Criados != 1 || res.JaSalvos != 2 || a.gerarLinks() != 1 {
		t.Fatalf("importar tudo: %+v", res)
	}
	a.exigir("ana", http.MethodGet, base+"/colecoes", nil, &cs, 200)
	if len(cs) != 1 || cs[0].Itens != 3 {
		t.Fatalf("coleções depois de importar tudo: %+v", cs)
	}
	a.exigirErro("ana", http.MethodPost, lista+"/importar", map[string]any{"produto_ids": []uuid.UUID{a.produtoID(5)}}, 422, "dados_invalidos")

	// O beto, sem a Shopee conectada, importa sem coleção: link pendente.
	a.exigir("beto", http.MethodPost, lista+"/importar", map[string]any{"colecao": false}, &res, 200)
	if res.Criados != 3 || res.ColecaoID != nil || res.LinkStatus != colecoes.LinkPendente || a.gerarLinks() != 0 {
		t.Fatalf("importação do beto: %+v", res)
	}

	// O mentor vê quem importou.
	var p curadoria.Painel
	a.exigir("mestre", http.MethodGet, lista+"/painel", nil, &p, 200)
	if p.Afiliados != 2 || len(p.Importadores) != 2 {
		t.Fatalf("painel: %+v", p)
	}
	a.exigir("mestre", http.MethodGet, lista, nil, &lv, 200)
	if *lv.Importadores != 2 || *lv.Itens[0].Importadores != 2 {
		t.Fatalf("lista vista pelo mestre: %+v", lv)
	}
	a.exigirErro("ana", http.MethodGet, lista+"/painel", nil, 403, "sem_permissao")

	// Notificações lidas.
	var c notificacoes.Caixa
	a.exigir("ana", http.MethodGet, base+"/notificacoes", nil, &c, 200)
	a.exigir("ana", http.MethodPost, base+"/notificacoes/"+c.Notificacoes[0].ID.String()+"/lida", nil, nil, http.StatusNoContent)
	a.exigir("ana", http.MethodGet, base+"/notificacoes", nil, &c, 200)
	if c.NaoLidas != 0 || c.Notificacoes[0].LidaEm == nil {
		t.Fatalf("depois de ler: %+v", c)
	}
	a.exigirErro("beto", http.MethodPost, base+"/notificacoes/"+c.Notificacoes[0].ID.String()+"/lida", nil, 404, "notificacao_nao_encontrada")
	a.exigir("beto", http.MethodPost, base+"/notificacoes/lidas", nil, nil, http.StatusNoContent)
	a.exigir("beto", http.MethodGet, base+"/notificacoes", nil, &c, 200)
	if c.NaoLidas != 0 {
		t.Fatalf("beto depois de ler todas: %+v", c)
	}

	// Apagar a lista não mexe na coleção de quem importou.
	a.exigir("mestre", http.MethodDelete, lista, nil, nil, http.StatusNoContent)
	a.exigir("ana", http.MethodGet, base+"/colecoes", nil, &cs, 200)
	if len(cs) != 1 || cs[0].Itens != 3 {
		t.Fatalf("coleção depois de apagar a lista: %+v", cs)
	}
}

func TestPermissoesEValidacoes(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mestre", "ana")
	base := "/v1/workspaces/" + ws

	// Afiliado não monta listas; workspace pessoal não tem curadoria.
	a.exigirErro("ana", http.MethodPost, base+"/listas", map[string]any{"titulo": "Minha"}, 403, "sem_permissao")
	var pessoal []contas.Workspace
	a.exigir("mestre", http.MethodGet, "/v1/workspaces", nil, &pessoal, 200)
	a.exigirErro("mestre", http.MethodPost, "/v1/workspaces/"+pessoal[0].ID.String()+"/listas", map[string]any{"titulo": "x"}, 409, "so_mentoria")

	a.exigirErro("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": " "}, 422, "dados_invalidos")
	a.exigirErro("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": strings.Repeat("a", 121)}, 422, "dados_invalidos")
	a.exigirErro("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "x", "outro": 1}, 400, "json_invalido")

	var l curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "Natal"}, &l, 201)
	lista := base + "/listas/" + l.ID.String()
	a.exigirErro("mestre", http.MethodPost, lista+"/publicar", nil, 409, "lista_vazia")
	a.exigirErro("mestre", http.MethodPost, lista+"/importar", map[string]any{}, 409, "lista_nao_publicada")
	a.exigirErro("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": uuid.New()}, 404, "produto_nao_encontrado")
	a.exigirErro("mestre", http.MethodPost, lista+"/itens", map[string]any{"url": "https://s.shopee.com.br/abc"}, 422, "link_curto")
	a.exigirErro("mestre", http.MethodPost, lista+"/itens", map[string]any{}, 422, "dados_invalidos")
	a.exigirErro("mestre", http.MethodPatch, lista+"/itens/"+a.produtoID(0).String(), map[string]any{"comentario": "x"}, 404, "item_lista_nao_encontrado")
	a.exigirErro("mestre", http.MethodDelete, lista+"/itens/"+a.produtoID(0).String(), nil, 404, "item_lista_nao_encontrado")
	a.exigirErro("mestre", http.MethodGet, base+"/listas/"+uuid.NewString(), nil, 404, "lista_nao_encontrada")
	a.exigirErro("mestre", http.MethodGet, base+"/listas/nao-e-uuid", nil, 404, "lista_nao_encontrada")

	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(0)}, &l, 201)
	a.exigir("mestre", http.MethodPost, lista+"/itens", map[string]any{"produto_id": a.produtoID(1)}, &l, 201)
	a.exigir("mestre", http.MethodDelete, lista+"/itens/"+a.produtoID(0).String(), nil, &l, 200)
	if len(l.Itens) != 1 || l.Itens[0].Produto.ID != a.produtoID(1) {
		t.Fatalf("depois de remover: %+v", l.Itens)
	}
	a.exigir("mestre", http.MethodPatch, lista, map[string]any{"descricao": "Presentes"}, &l, 200)
	if l.Titulo != "Natal" || l.Descricao != "Presentes" {
		t.Fatalf("editar: %+v", l.Lista)
	}
	a.exigir("mestre", http.MethodPost, lista+"/publicar", nil, &l, 200)

	// O afiliado só lê e importa.
	a.exigir("ana", http.MethodGet, lista, nil, &l, 200)
	for _, c := range []struct{ metodo, caminho string }{
		{http.MethodPatch, lista},
		{http.MethodDelete, lista},
		{http.MethodPost, lista + "/itens"},
		{http.MethodPost, lista + "/publicar"},
		{http.MethodPut, lista + "/ordem"},
		{http.MethodDelete, lista + "/itens/" + a.produtoID(1).String()},
	} {
		corpo := map[string]any{}
		if c.metodo == http.MethodDelete || strings.HasSuffix(c.caminho, "/publicar") {
			corpo = nil
		}
		a.exigirErro("ana", c.metodo, c.caminho, corpo, 403, "sem_permissao")
	}

	// Limite de listas do plano.
	if _, err := a.pool.Exec(context.Background(), "UPDATE limites SET valor = 1 WHERE plano = 'mentoria' AND chave = 'listas'"); err != nil {
		t.Fatal(err)
	}
	a.exigirErro("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "Outra"}, 409, "limite_listas")
}

// TestVazamento confere que listas, importações e notificações não vazam
// entre workspaces nem entre membros, pela API e direto no banco (RLS).
func TestVazamento(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	ws := a.mentoria("mestre", "ana", "beto")
	outro := a.mentoria("rival", "carla")
	base := "/v1/workspaces/" + ws

	var rascunho, publicada curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "Rascunho"}, &rascunho, 201)
	a.exigir("mestre", http.MethodPost, base+"/listas/"+rascunho.ID.String()+"/itens", map[string]any{"produto_id": a.produtoID(0)}, nil, 201)
	a.exigir("mestre", http.MethodPost, base+"/listas", map[string]any{"titulo": "Publicada"}, &publicada, 201)
	a.exigir("mestre", http.MethodPost, base+"/listas/"+publicada.ID.String()+"/itens", map[string]any{"produto_id": a.produtoID(1)}, nil, 201)
	a.exigir("mestre", http.MethodPost, base+"/listas/"+publicada.ID.String()+"/publicar", nil, nil, 200)
	a.entregar()
	a.exigir("ana", http.MethodPost, base+"/listas/"+publicada.ID.String()+"/importar", map[string]any{}, nil, 200)
	a.exigir("ana", http.MethodPost, "/v1/eu/push", map[string]any{"endpoint": endpointPush, "keys": map[string]string{"p256dh": "k", "auth": "a"}}, nil, http.StatusNoContent)

	// Outro workspace não vê nada, nem pelo caminho certo da lista.
	var ls []curadoria.Lista
	a.exigir("rival", http.MethodGet, "/v1/workspaces/"+outro+"/listas", nil, &ls, 200)
	if len(ls) != 0 {
		t.Fatalf("o rival vê listas de outra mentoria: %+v", ls)
	}
	a.exigirErro("rival", http.MethodGet, "/v1/workspaces/"+outro+"/listas/"+publicada.ID.String(), nil, 404, "lista_nao_encontrada")
	a.exigirErro("rival", http.MethodPatch, "/v1/workspaces/"+outro+"/listas/"+publicada.ID.String(), map[string]any{"titulo": "x"}, 404, "lista_nao_encontrada")
	a.exigirErro("rival", http.MethodDelete, "/v1/workspaces/"+outro+"/listas/"+rascunho.ID.String(), nil, 404, "lista_nao_encontrada")
	a.exigirErro("carla", http.MethodPost, "/v1/workspaces/"+outro+"/listas/"+publicada.ID.String()+"/importar", map[string]any{}, 404, "lista_nao_encontrada")
	a.exigirErro("rival", http.MethodGet, base+"/listas", nil, 404, "workspace_nao_encontrado")
	var c notificacoes.Caixa
	a.exigir("carla", http.MethodGet, "/v1/workspaces/"+outro+"/notificacoes", nil, &c, 200)
	if len(c.Notificacoes) != 0 {
		t.Fatalf("carla recebeu aviso de outra mentoria: %+v", c)
	}

	contar := func(e postgres.Escopo, tabela string) int {
		t.Helper()
		var n int
		err := postgres.InTx(ctx, a.pool, e, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+tabela).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	esc := func(sub, workspace string) postgres.Escopo {
		return postgres.Escopo{UsuarioID: a.usuario(sub).ID.String(), WorkspaceID: workspace}
	}
	for _, c := range []struct {
		nome   string
		e      postgres.Escopo
		tabela string
		quer   int
	}{
		{"mentor vê rascunho e publicada", esc("mestre", ws), "listas_curadoria", 2},
		{"afiliado só vê a publicada", esc("beto", ws), "listas_curadoria", 1},
		{"afiliado só vê itens da publicada", esc("beto", ws), "lista_itens", 1},
		{"outro workspace não vê listas", esc("rival", outro), "listas_curadoria", 0},
		{"mentor de outro workspace no workspace errado", esc("rival", ws), "listas_curadoria", 1},
		{"sem workspace não vê listas", postgres.Escopo{UsuarioID: a.usuario("mestre").ID.String()}, "listas_curadoria", 0},
		{"mentor vê as importações da turma", esc("mestre", ws), "importacoes", 1},
		{"ana vê a importação dela", esc("ana", ws), "importacoes", 1},
		{"beto não vê a importação da ana", esc("beto", ws), "importacoes", 0},
		{"ana vê a notificação dela", esc("ana", ws), "notificacoes", 1},
		{"beto vê só a dele", esc("beto", ws), "notificacoes", 1},
		{"mentor não vê notificações da turma", esc("mestre", ws), "notificacoes", 0},
		{"ana vê a inscrição de push dela", esc("ana", ws), "push_inscricoes", 1},
		{"beto não vê a inscrição da ana", esc("beto", ws), "push_inscricoes", 0},
	} {
		if n := contar(c.e, c.tabela); n != c.quer {
			t.Errorf("%s: %d linhas em %s, quer %d", c.nome, n, c.tabela, c.quer)
		}
	}

	// Gravar com o papel da aplicação: afiliado não cria lista nem importa em
	// nome de outro, e ninguém importa um rascunho.
	gravar := func(e postgres.Escopo, sql string, args ...any) error {
		return postgres.InTx(ctx, a.pool, e, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	ana := a.usuario("ana").ID
	for nome, err := range map[string]error{
		"afiliado cria lista": gravar(esc("ana", ws),
			"INSERT INTO listas_curadoria (workspace_id, autor_id, titulo) VALUES ($1, $2, 'x')", ws, ana),
		"importação em nome da ana": gravar(esc("beto", ws),
			"INSERT INTO importacoes (lista_id, workspace_id, usuario_id, produto_id) VALUES ($1, $2, $3, $4)", publicada.ID, ws, ana, a.produtoID(1)),
		"importação de rascunho": gravar(esc("ana", ws),
			"INSERT INTO importacoes (lista_id, workspace_id, usuario_id, produto_id) VALUES ($1, $2, $3, $4)", rascunho.ID, ws, ana, a.produtoID(0)),
		"notificação para outro": gravar(esc("beto", ws),
			"INSERT INTO notificacoes (workspace_id, usuario_id, tipo, chave, titulo) VALUES ($1, $2, 't', 'k', 'x')", ws, ana),
	} {
		if err == nil || !strings.Contains(err.Error(), "row-level security") {
			t.Errorf("%s: %v", nome, err)
		}
	}
	// Afiliado não edita nem apaga lista (a política esconde as linhas).
	for _, sql := range []string{
		"UPDATE listas_curadoria SET titulo = 'invadida'",
		"DELETE FROM lista_itens",
		"DELETE FROM listas_curadoria",
	} {
		if err := gravar(esc("ana", ws), sql); err != nil {
			t.Fatal(err)
		}
	}
	if n := contar(esc("mestre", ws), "listas_curadoria"); n != 2 {
		t.Fatalf("o afiliado apagou listas: sobraram %d", n)
	}
	var l curadoria.ListaDetalhe
	a.exigir("mestre", http.MethodGet, base+"/listas/"+publicada.ID.String(), nil, &l, 200)
	if l.Titulo != "Publicada" || len(l.Itens) != 1 {
		t.Fatalf("o afiliado alterou a lista: %+v", l)
	}
}
