package shopee_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
)

func TestParseLink(t *testing.T) {
	valid := map[string][2]int64{
		"https://shopee.com.br/Fone-Bluetooth-i.123456.7890123":               {123456, 7890123},
		"https://shopee.com.br/Fone-i.123456.7890123?sp_atk=abc&xptdk=def":    {123456, 7890123},
		"shopee.com.br/product/123456/7890123":                                {123456, 7890123},
		"https://www.shopee.com.br/product/1/2/":                              {1, 2},
		"  https://m.shopee.com.br/Kit-3-Camisetas-Algodão-i.99.1000000001  ": {99, 1000000001},
	}
	for link, want := range valid {
		shop, item, err := shopee.ParseLink(link)
		if err != nil || shop != want[0] || item != want[1] {
			t.Errorf("%q: %d %d %v", link, shop, item, err)
		}
		p, err := shopee.LinkParser{}.ParseProductLink(link)
		if err != nil || p != (domain.ProductLink{Source: domain.SourceShopee, ShopID: want[0], ItemID: want[1]}) {
			t.Errorf("%q: %+v %v", link, p, err)
		}
	}
	for _, link := range []string{"https://s.shopee.com.br/abc", "https://shope.ee/xyz"} {
		if _, err := (shopee.LinkParser{}).ParseProductLink(link); !errors.Is(err, domain.ErrShortLink) {
			t.Errorf("%q: %v, want short link", link, err)
		}
	}
	for _, link := range []string{
		"", "abc", "https://shopee.com.br/", "https://shopee.com.br/loja-oficial",
		"https://exemplo.com/Produto-i.1.2", "https://shopee.com.br.golpe.com/Produto-i.1.2",
		"ftp://shopee.com.br/product/1/2", "https://shopee.com.br/product/1/0",
	} {
		if _, err := (shopee.LinkParser{}).ParseProductLink(link); !errors.Is(err, domain.ErrInvalidProductLink) {
			t.Errorf("%q: %v, want invalid link", link, err)
		}
	}
}

func TestGenerateLinkMock(t *testing.T) {
	ctx := context.Background()
	c := shopee.NewMock(&shopee.Mock{Secrets: map[string]string{"123": "s"}}, shopee.Config{})
	cred := shopee.Credential{AppID: "123", Secret: "s"}
	origin := `https://shopee.com.br/product/1/2?q="quotes"`

	a, err := c.GenerateLink(ctx, cred, origin, []string{"instagram", "w123"})
	if err != nil || !strings.HasPrefix(a, "https://s.shopee.com.br/") {
		t.Fatalf("%q %v", a, err)
	}
	b, _ := c.GenerateLink(ctx, cred, origin, []string{"tiktok", "w123"})
	again, _ := c.GenerateLink(ctx, cred, origin, []string{"instagram", "w123"})
	if a == b || a != again {
		t.Fatalf("links: %q %q %q", a, b, again)
	}
	if _, err := c.GenerateLink(ctx, cred, origin, []string{"with space"}); err == nil {
		t.Fatal("invalid subId accepted")
	}
	if _, err := c.GenerateLink(ctx, shopee.Credential{AppID: "123", Secret: "wrong"}, origin, nil); !errors.Is(err, domain.ErrSourceInvalidCredential) {
		t.Fatalf("wrong secret: %v", err)
	}
}

func TestOfferByItemMock(t *testing.T) {
	ctx := context.Background()
	c := shopee.NewMock(&shopee.Mock{}, shopee.Config{})
	cat := shopee.AppCatalog{Client: c, Credential: shopee.Credential{AppID: "1", Secret: "x"}}
	p, err := cat.Offers(ctx, domain.CatalogFilter{Page: 2, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := p.Offers[3]
	o, err := cat.OfferByItem(ctx, want.ItemID)
	if err != nil || o.ItemID != want.ItemID || o.Name != want.Name {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := cat.OfferByItem(ctx, 42); !errors.Is(err, domain.ErrSourceNotFound) {
		t.Fatalf("missing item: %v", err)
	}
}
