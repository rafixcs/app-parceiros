package shopee

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

var (
	// .../Product-name-i.123456.7890123
	reNameLink = regexp.MustCompile(`-i\.(\d{1,20})\.(\d{1,20})$`)
	// .../product/123456/7890123
	reProductLink = regexp.MustCompile(`^/product/(\d{1,20})/(\d{1,20})/?$`)
)

// LinkParser reads Shopee Brazil product links (domain.LinkParser).
type LinkParser struct{}

var _ domain.LinkParser = LinkParser{}

func (LinkParser) ParseProductLink(raw string) (domain.ProductLink, error) {
	shopID, itemID, err := ParseLink(raw)
	if err != nil {
		return domain.ProductLink{}, err
	}
	return domain.ProductLink{Source: domain.SourceShopee, ShopID: shopID, ItemID: itemID}, nil
}

// ParseLink extracts the shopId and the itemId of a Shopee Brazil product
// link, as the ones in the browser bar or in the app:
//
//	https://shopee.com.br/Fone-Bluetooth-i.123456.7890123?sp_atk=...
//	https://shopee.com.br/product/123456/7890123
//
// A shortened link (s.shopee.com.br, shope.ee), which only reveals the
// product when opened in the browser, returns domain.ErrShortLink; anything
// else that is not a product page, domain.ErrInvalidProductLink.
func ParseLink(raw string) (shopID, itemID int64, err error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return 0, 0, domain.ErrInvalidProductLink
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "shopee.com.br", "m.shopee.com.br":
	case "s.shopee.com.br", "shope.ee", "shp.ee":
		return 0, 0, domain.ErrShortLink
	default:
		return 0, 0, domain.ErrInvalidProductLink
	}
	var m []string
	if m = reProductLink.FindStringSubmatch(u.Path); m == nil {
		m = reNameLink.FindStringSubmatch(u.Path)
	}
	if m == nil {
		return 0, 0, domain.ErrInvalidProductLink
	}
	shopID, err1 := strconv.ParseInt(m[1], 10, 64)
	itemID, err2 := strconv.ParseInt(m[2], 10, 64)
	if err1 != nil || err2 != nil || itemID <= 0 {
		return 0, 0, domain.ErrInvalidProductLink
	}
	return shopID, itemID, nil
}
