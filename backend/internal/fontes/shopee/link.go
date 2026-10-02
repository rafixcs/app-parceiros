package shopee

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrLinkInvalido: o texto não é um link de produto da Shopee Brasil.
	ErrLinkInvalido = errors.New("link de produto da Shopee inválido")
	// ErrLinkCurto: link encurtado (s.shopee.com.br, shope.ee), que só
	// revela o produto abrindo no navegador.
	ErrLinkCurto = errors.New("link curto da Shopee")
)

var (
	// .../Nome-do-produto-i.123456.7890123
	reLinkNome = regexp.MustCompile(`-i\.(\d{1,20})\.(\d{1,20})$`)
	// .../product/123456/7890123
	reLinkProduto = regexp.MustCompile(`^/product/(\d{1,20})/(\d{1,20})/?$`)
)

// LerLink extrai o shopId e o itemId de um link de produto da Shopee Brasil,
// como os que aparecem na barra do navegador ou no app:
//
//	https://shopee.com.br/Fone-Bluetooth-i.123456.7890123?sp_atk=...
//	https://shopee.com.br/product/123456/7890123
func LerLink(raw string) (lojaID, itemID int64, err error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return 0, 0, ErrLinkInvalido
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "shopee.com.br", "m.shopee.com.br":
	case "s.shopee.com.br", "shope.ee", "shp.ee":
		return 0, 0, ErrLinkCurto
	default:
		return 0, 0, ErrLinkInvalido
	}
	var m []string
	if m = reLinkProduto.FindStringSubmatch(u.Path); m == nil {
		m = reLinkNome.FindStringSubmatch(u.Path)
	}
	if m == nil {
		return 0, 0, ErrLinkInvalido
	}
	lojaID, err1 := strconv.ParseInt(m[1], 10, 64)
	itemID, err2 := strconv.ParseInt(m[2], 10, 64)
	if err1 != nil || err2 != nil || itemID <= 0 {
		return 0, 0, ErrLinkInvalido
	}
	return lojaID, itemID, nil
}
