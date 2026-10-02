package shopee_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
)

func TestLerLink(t *testing.T) {
	validos := map[string][2]int64{
		"https://shopee.com.br/Fone-Bluetooth-i.123456.7890123":               {123456, 7890123},
		"https://shopee.com.br/Fone-i.123456.7890123?sp_atk=abc&xptdk=def":    {123456, 7890123},
		"shopee.com.br/product/123456/7890123":                                {123456, 7890123},
		"https://www.shopee.com.br/product/1/2/":                              {1, 2},
		"  https://m.shopee.com.br/Kit-3-Camisetas-Algodão-i.99.1000000001  ": {99, 1000000001},
	}
	for link, want := range validos {
		loja, item, err := shopee.LerLink(link)
		if err != nil || loja != want[0] || item != want[1] {
			t.Errorf("%q: %d %d %v", link, loja, item, err)
		}
	}
	for _, link := range []string{"https://s.shopee.com.br/abc", "https://shope.ee/xyz"} {
		if _, _, err := shopee.LerLink(link); !errors.Is(err, shopee.ErrLinkCurto) {
			t.Errorf("%q: %v, quer link curto", link, err)
		}
	}
	for _, link := range []string{
		"", "abc", "https://shopee.com.br/", "https://shopee.com.br/loja-oficial",
		"https://exemplo.com/Produto-i.1.2", "https://shopee.com.br.golpe.com/Produto-i.1.2",
		"ftp://shopee.com.br/product/1/2", "https://shopee.com.br/product/1/0",
	} {
		if _, _, err := shopee.LerLink(link); !errors.Is(err, shopee.ErrLinkInvalido) {
			t.Errorf("%q: %v, quer link inválido", link, err)
		}
	}
}

func TestGerarLinkMock(t *testing.T) {
	ctx := context.Background()
	c := shopee.NovoMock(&shopee.Mock{Segredos: map[string]string{"123": "s"}}, shopee.Config{})
	cred := shopee.Credencial{AppID: "123", Secret: "s"}
	origem := `https://shopee.com.br/product/1/2?q="aspas"`

	a, err := c.GerarLink(ctx, cred, origem, []string{"instagram", "w123"})
	if err != nil || !strings.HasPrefix(a, "https://s.shopee.com.br/") {
		t.Fatalf("%q %v", a, err)
	}
	b, _ := c.GerarLink(ctx, cred, origem, []string{"tiktok", "w123"})
	again, _ := c.GerarLink(ctx, cred, origem, []string{"instagram", "w123"})
	if a == b || a != again {
		t.Fatalf("links: %q %q %q", a, b, again)
	}
	if _, err := c.GerarLink(ctx, cred, origem, []string{"com espaço"}); err == nil {
		t.Fatal("subId inválido aceito")
	}
	if _, err := c.GerarLink(ctx, shopee.Credencial{AppID: "123", Secret: "errado"}, origem, nil); !errors.Is(err, fontes.ErrCredencialInvalida) {
		t.Fatalf("secret errado: %v", err)
	}
}

func TestOfertaPorItemMock(t *testing.T) {
	ctx := context.Background()
	c := shopee.NovoMock(&shopee.Mock{}, shopee.Config{})
	cred := shopee.Credencial{AppID: "1", Secret: "x"}
	p, err := c.Ofertas(ctx, cred, shopee.FiltroOfertas{Pagina: 2, Limite: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := p.Ofertas[3]
	o, err := c.OfertaPorItem(ctx, cred, want.ItemID)
	if err != nil || o.ItemID != want.ItemID || o.Nome != want.Nome {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := c.OfertaPorItem(ctx, cred, 42); !errors.Is(err, fontes.ErrNaoEncontrado) {
		t.Fatalf("item inexistente: %v", err)
	}
}
