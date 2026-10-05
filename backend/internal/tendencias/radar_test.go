package tendencias_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/tendencias"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type ambiente struct {
	t      *testing.T
	router http.Handler
}

func (a *ambiente) chamar(sub, caminho string, out any) int {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodGet, caminho, nil)
	req.Header.Set("Authorization", "Bearer dev:"+sub)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s: %q: %v", caminho, rec.Body.String(), err)
		}
	}
	return rec.Code
}

func (a *ambiente) pessoal(sub string) string {
	a.t.Helper()
	var ws []contas.Workspace
	if st := a.chamar(sub, "/v1/workspaces", &ws); st != 200 || len(ws) == 0 {
		a.t.Fatalf("workspaces de %s: %d", sub, st)
	}
	return ws[0].ID.String()
}

// TestRadar roda o fluxo do M2 de ponta a ponta com o mock da Shopee: duas
// coletas com 7 dias de intervalo, o cálculo de tendências e as rotas do radar.
func TestRadar(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	produtosSvc := produtos.NewService(pool)
	radar := tendencias.NewService(pool, produtosSvc)

	cats, err := shopee.Categorias()
	if err != nil {
		t.Fatal(err)
	}
	var cs []produtos.Categoria
	for _, c := range cats {
		cs = append(cs, produtos.Categoria{ID: c.ID, Nome: c.Nome, Monitorar: true})
	}
	if err := produtosSvc.SalvarCategorias(ctx, fontes.Shopee, cs); err != nil {
		t.Fatal(err)
	}
	monitoradas, err := produtosSvc.CategoriasMonitoradas(ctx, fontes.Shopee)
	if err != nil || len(monitoradas) != len(cats) {
		t.Fatalf("monitoradas: %v %v", monitoradas, err)
	}

	agora := time.Now().UTC().Truncate(time.Hour)
	mock := &shopee.Mock{Evoluir: true, Agora: func() time.Time { return agora }}
	bucket := &storage.Memory{}
	depois := 0
	w := &produtos.SnapshotCatalogoWorker{
		Svc:      produtosSvc,
		Catalogo: shopee.CatalogoDoApp{Cliente: shopee.NovoMock(mock, shopee.Config{}), Credencial: shopee.Credencial{AppID: "1", Secret: "x"}},
		Storage:  bucket,
		Log:      log,
		Depois:   func(context.Context) error { depois++; return nil },
		Agora:    func() time.Time { return agora },
	}
	coletar := func() {
		t.Helper()
		job := &river.Job[produtos.SnapshotCatalogoArgs]{
			JobRow: &rivertype.JobRow{Kind: "snapshot_catalogo"},
			Args:   produtos.SnapshotCatalogoArgs{CategoriaID: 0, Paginas: 10},
		}
		if err := w.Work(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	coletar()
	if n, err := radar.Calcular(ctx, agora); err != nil || n != 80 {
		t.Fatalf("primeiro cálculo: %d %v", n, err)
	}
	agora = agora.Add(7 * 24 * time.Hour)
	coletar()
	if n, err := radar.Calcular(ctx, agora); err != nil || n != 80 {
		t.Fatalf("segundo cálculo: %d %v", n, err)
	}
	if depois != 2 {
		t.Fatalf("Depois rodou %d vezes", depois)
	}
	if len(bucket.Keys()) != 4 { // 80 itens = 2 páginas por coleta
		t.Fatalf("respostas brutas guardadas: %v", bucket.Keys())
	}

	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(contas.NewService(pool, auth.Dev{}, "https://app.teste"), log).
		Rotas(r, auth.Dev{}, tendencias.NewHandler(radar, log).Modulo())
	a := &ambiente{t: t, router: r}
	ws := a.pessoal("ana")
	base := "/v1/workspaces/" + ws + "/radar"

	var p tendencias.Pagina
	if st := a.chamar("ana", base, &p); st != 200 {
		t.Fatalf("radar: %d", st)
	}
	if p.Total != 80 || len(p.Itens) != tendencias.PorPaginaPadrao || p.AtualizadoEm == nil || !p.AtualizadoEm.Equal(agora) {
		t.Fatalf("radar: total %d, itens %d, atualizado %v", p.Total, len(p.Itens), p.AtualizadoEm)
	}
	if p.Itens[0].Score != 100 || p.Itens[0].Vendas7d == nil || *p.Itens[0].Vendas7d <= 0 {
		t.Fatalf("primeiro item: %+v", p.Itens[0])
	}
	for i := 1; i < len(p.Itens); i++ {
		if p.Itens[i].Score > p.Itens[i-1].Score {
			t.Fatal("radar fora da ordem de tendência")
		}
	}
	it := p.Itens[0]
	if it.GanhoPorVendaCentavos != tendencias.GanhoPorVenda(it.PrecoMinCentavos, it.ComissaoBP) {
		t.Fatalf("ganho por venda: %+v", it)
	}

	t.Run("ordenações", func(t *testing.T) {
		campo := map[string]func(tendencias.Item) int64{
			"comissao": func(i tendencias.Item) int64 { return int64(i.ComissaoBP) },
			"ganho":    func(i tendencias.Item) int64 { return i.GanhoPorVendaCentavos },
			"vendas":   func(i tendencias.Item) int64 { return i.Vendas },
		}
		for ordem, f := range campo {
			var p tendencias.Pagina
			a.chamar("ana", base+"?por_pagina=50&ordem="+ordem, &p)
			for i := 1; i < len(p.Itens); i++ {
				if f(p.Itens[i]) > f(p.Itens[i-1]) {
					t.Fatalf("%s fora de ordem na posição %d", ordem, i)
				}
			}
		}
	})

	t.Run("filtros", func(t *testing.T) {
		var p tendencias.Pagina
		q := url.Values{"categoria": {"100004"}, "comissao_min": {"700"}, "nota_min": {"4.5"},
			"preco_min": {"2000"}, "preco_max": {"20000"}, "por_pagina": {"50"}}
		a.chamar("ana", base+"?"+q.Encode(), &p)
		if p.Total == 0 {
			t.Fatal("filtro sem resultado; ajuste o caso")
		}
		for _, i := range p.Itens {
			if i.Categorias[0] != 100004 || i.ComissaoBP < 700 || i.Nota == nil || *i.Nota < 4.5 ||
				i.PrecoMinCentavos < 2000 || i.PrecoMinCentavos > 20000 {
				t.Fatalf("item fora do filtro: %+v", i)
			}
		}

		a.chamar("ana", base+"?q="+url.QueryEscape("fone bluetooth"), &p)
		if p.Total != 1 || !strings.Contains(p.Itens[0].Nome, "Fone bluetooth") {
			t.Fatalf("busca: %d %+v", p.Total, p.Itens)
		}
		a.chamar("ana", base+"?q="+url.QueryEscape("100%_"), &p)
		if p.Total != 0 {
			t.Fatalf("curinga na busca: %d", p.Total)
		}
		a.chamar("ana", base+"?pagina=4&por_pagina=24", &p)
		if len(p.Itens) != 8 || p.Total != 80 {
			t.Fatalf("última página: %d itens", len(p.Itens))
		}

		var e httputil.ErrorBody
		for _, ruim := range []string{"ordem=preco", "por_pagina=51", "nota_min=6", "comissao_min=x"} {
			if st := a.chamar("ana", base+"?"+ruim, &e); st != 422 || e.Code != "dados_invalidos" {
				t.Errorf("%s: %d %+v", ruim, st, e)
			}
		}
	})

	t.Run("categorias", func(t *testing.T) {
		var cs []tendencias.Categoria
		a.chamar("ana", base+"/categorias", &cs)
		if len(cs) != 5 || cs[0].Produtos != 16 || cs[0].Nome == "" || strings.HasPrefix(cs[0].Nome, "Categoria ") {
			t.Fatalf("%+v", cs)
		}
	})

	t.Run("detalhe com histórico", func(t *testing.T) {
		var d tendencias.Detalhe
		if st := a.chamar("ana", base+"/produtos/"+it.ProdutoID.String()+"?dias=90", &d); st != 200 {
			t.Fatalf("detalhe: %d", st)
		}
		if d.Produto.ProdutoID != it.ProdutoID || len(d.Historico) != 2 || d.Historico[1].Vendas <= d.Historico[0].Vendas {
			t.Fatalf("%+v", d)
		}
		var e httputil.ErrorBody
		if st := a.chamar("ana", base+"/produtos/00000000-0000-0000-0000-000000000000", &e); st != 404 || e.Code != "produto_nao_encontrado" {
			t.Fatalf("produto inexistente: %d %+v", st, e)
		}
	})

	t.Run("só membros do workspace", func(t *testing.T) {
		var e httputil.ErrorBody
		for _, c := range []string{base, base + "/categorias", base + "/produtos/" + it.ProdutoID.String()} {
			if st := a.chamar("bia", c, &e); st != 404 || e.Code != "workspace_nao_encontrado" {
				t.Fatalf("bia em %s: %d %+v", c, st, e)
			}
		}
		if st := a.chamar("", base, nil); st != 401 {
			t.Fatalf("sem login: %d", st)
		}
	})

	t.Run("produto sem coleta recente sai do radar", func(t *testing.T) {
		if _, err := radar.Calcular(ctx, agora.Add(3*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		var p tendencias.Pagina
		a.chamar("ana", base, &p)
		if p.Total != 0 {
			t.Fatalf("radar com %d produtos velhos", p.Total)
		}
		var d tendencias.Detalhe
		if st := a.chamar("ana", base+"/produtos/"+it.ProdutoID.String(), &d); st != 200 || d.Produto.Score != 0 {
			t.Fatalf("detalhe fora do radar: %d %+v", st, d.Produto)
		}
	})
}
