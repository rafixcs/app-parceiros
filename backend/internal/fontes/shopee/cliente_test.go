package shopee

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
)

func TestAssinar(t *testing.T) {
	// Vetor calculado fora do Go (python hashlib): sha256("123" + "1700000000" + `{"query":"x"}` + "abc").
	got := Assinar(Credencial{AppID: "123", Secret: "abc"}, time.Unix(1700000000, 0), []byte(`{"query":"x"}`))
	want := "SHA256 Credential=123, Timestamp=1700000000, Signature=09f81a15291db81967cc0fca18bbc9144059c8ccac44d050e5ce2749f509529d"
	if got != want {
		t.Fatalf("assinatura %q, quer %q", got, want)
	}
}

func TestDecimal(t *testing.T) {
	casos := []struct {
		in    string
		casas int
		want  int64
	}{
		{"129.9", 2, 12990}, {"129.90", 2, 12990}, {"19", 2, 1900}, {"0.125", 4, 1250},
		{"0.1", 4, 1000}, {"4.85", 2, 485}, {"0.12345", 4, 1235}, {"", 2, 0}, {"1500", 0, 1500},
	}
	for _, c := range casos {
		got, err := decimal(c.in).escalar(c.casas)
		if err != nil || got != c.want {
			t.Errorf("%q/%d: %d %v, quer %d", c.in, c.casas, got, err, c.want)
		}
	}
	for _, ruim := range []string{"abc", "1e5", "1.2.3"} {
		if _, err := decimal(ruim).escalar(2); err == nil {
			t.Errorf("%q aceito", ruim)
		}
	}
}

func TestOfertasMock(t *testing.T) {
	ctx := context.Background()
	m := &Mock{Segredos: map[string]string{"111": "segredo"}}
	c := NovoMock(m, Config{})
	cred := Credencial{AppID: "111", Secret: "segredo"}

	p, err := c.Ofertas(ctx, cred, FiltroOfertas{CategoriaID: 100004, Pagina: 1, Limite: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Ofertas) != 10 || !p.TemProxima || len(p.Bruto) == 0 {
		t.Fatalf("página 1: %d ofertas, próxima %v", len(p.Ofertas), p.TemProxima)
	}
	for i, o := range p.Ofertas {
		if o.Categorias[0] != 100004 || o.ItemID == 0 || o.PrecoMinCentavos == 0 || o.ComissaoBP == 0 || o.URL == "" {
			t.Fatalf("oferta %d incompleta: %+v", i, o)
		}
		if i > 0 && o.Vendas > p.Ofertas[i-1].Vendas {
			t.Fatal("não veio por mais vendidos")
		}
	}
	p2, err := c.Ofertas(ctx, cred, FiltroOfertas{CategoriaID: 100004, Pagina: 2, Limite: 10})
	if err != nil || len(p2.Ofertas) != 6 || p2.TemProxima {
		t.Fatalf("página 2: %d %v %v", len(p2.Ofertas), p2.TemProxima, err)
	}

	if err := c.Validar(ctx, Credencial{AppID: "111", Secret: "outro"}); !errors.Is(err, fontes.ErrCredencialInvalida) {
		t.Fatalf("secret errado: %v", err)
	}
}

func TestErrosDaAPI(t *testing.T) {
	ctx := context.Background()
	c := NovoMock(&Mock{}, Config{})
	casos := map[string]error{
		MockAppIDInvalido: fontes.ErrCredencialInvalida,
		MockAppIDLimite:   fontes.ErrLimite,
		MockAppIDNegado:   fontes.ErrAcessoNegado,
	}
	for app, want := range casos {
		err := c.Validar(ctx, Credencial{AppID: app, Secret: "s3gr3d0-n40-v4z4"})
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, quer %v", app, err, want)
		}
		if strings.Contains(err.Error(), "s3gr3d0") {
			t.Errorf("%s: secret no erro", app)
		}
	}
	if err := c.Validar(ctx, Credencial{AppID: "999", Secret: "x"}); err != nil {
		t.Fatalf("AppID qualquer: %v", err)
	}
}

func TestCredencialNaoVazaSecret(t *testing.T) {
	cred := Credencial{AppID: "1", Secret: "s3gr3d0"}
	for _, s := range []string{cred.String(), cred.GoString()} {
		if strings.Contains(s, "s3gr3d0") {
			t.Fatalf("secret em %q", s)
		}
	}
}

func TestMockEvolui(t *testing.T) {
	ctx := context.Background()
	agora := dataGravacao
	m := &Mock{Evoluir: true, Agora: func() time.Time { return agora }}
	c := NovoMock(m, Config{})
	cred := Credencial{AppID: "1", Secret: "x"}
	antes, err := c.Ofertas(ctx, cred, FiltroOfertas{Limite: 50})
	if err != nil {
		t.Fatal(err)
	}
	agora = agora.Add(7 * 24 * time.Hour)
	depois, _ := c.Ofertas(ctx, cred, FiltroOfertas{Limite: 50})
	var total0, total1 int64
	for i := range antes.Ofertas {
		total0 += antes.Ofertas[i].Vendas
		total1 += depois.Ofertas[i].Vendas
	}
	if total1 <= total0 {
		t.Fatalf("vendas não cresceram: %d -> %d", total0, total1)
	}
}
