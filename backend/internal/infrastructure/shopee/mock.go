package shopee

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"hash/fnv"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

//go:embed testdata/*.json
var testdata embed.FS

// AppIDs the mock treats as errors, with the Shopee code in the number
// itself.
const (
	MockAppIDInvalid      = "10020" // signature refused
	MockAppIDRateLimited  = "10030" // Shopee rate limit
	MockAppIDAccessDenied = "10031" // access denied
	// MockAppIDUnavailable answers HTTP 503, as Shopee down.
	MockAppIDUnavailable = "50300"
)

// Mock imitates the Shopee Open API from the answers recorded in testdata/.
// Any numeric AppID is accepted, except the MockAppID*. To check the
// signature of an AppID, register its Secret in Secrets.
//
// With Evolve, sales grow every day since the recording date, at a pace of
// each item's own, so the radar has trends in the local environment.
type Mock struct {
	Secrets map[string]string
	Evolve  bool
	Now     func() time.Time

	mu    sync.Mutex
	calls int
	// links keeps, per AppID, the generated links (product and subIds), so
	// conversionReport simulates sales through them.
	links map[string][]generatedLink
}

type generatedLink struct {
	itemID int64
	subID  string
}

// Date of the recording of testdata/product_offer_v2.json.
var recordedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// NewMock returns a Client that talks to the Mock instead of Shopee.
func NewMock(m *Mock, c Config) *Client {
	if m.Now == nil {
		m.Now = time.Now
	}
	c.URL = "https://mock.shopee.invalid/graphql"
	c.HTTP = &http.Client{Transport: m}
	return NewClient(c)
}

// Calls counts the requests received.
func (m *Mock) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// Categories returns the level-1 categories of the recorded catalog, all
// monitored, so the local radar has named filters.
func Categories() ([]domain.Category, error) {
	var rec []struct {
		ID   int64  `json:"id"`
		Name string `json:"nome"`
	}
	b, err := testdata.ReadFile("testdata/categorias.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	out := make([]domain.Category, len(rec))
	for i, c := range rec {
		out[i] = domain.Category{ID: c.ID, Name: c.Name, Monitored: true}
	}
	return out, nil
}

var (
	reAuth = regexp.MustCompile(`^SHA256 Credential=(\d+), Timestamp=(\d+), Signature=([0-9a-f]{64})$`)
	reArg  = regexp.MustCompile(`(productCatId|itemId|sortType|page|limit):(\d+)`)
	// originUrl:"...",subIds:[...] of generateShortLink.
	reLink = regexp.MustCompile(`originUrl:("(?:[^"\\]|\\.)*"),subIds:(\[[^\]]*\])`)
)

func (m *Mock) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	auth := reAuth.FindStringSubmatch(req.Header.Get("Authorization"))
	if auth == nil {
		return recorded("erro_10020.json")
	}
	appID := auth[1]
	switch appID {
	case MockAppIDInvalid:
		return recorded("erro_10020.json")
	case MockAppIDRateLimited:
		return recorded("erro_10030.json")
	case MockAppIDAccessDenied:
		return recorded("erro_10031.json")
	case MockAppIDUnavailable:
		return answer(http.StatusServiceUnavailable, []byte(`<html>503 Service Unavailable</html>`))
	}
	if secret, ok := m.Secrets[appID]; ok {
		ts, _ := strconv.ParseInt(auth[2], 10, 64)
		if Sign(Credential{AppID: appID, Secret: secret}, time.Unix(ts, 0), payload) != req.Header.Get("Authorization") {
			return recorded("erro_10020.json")
		}
	}

	var body struct {
		Query string `json:"query"`
	}
	err = json.Unmarshal(payload, &body)
	switch {
	case err == nil && strings.Contains(body.Query, "generateShortLink"):
		return m.link(appID, body.Query)
	case err == nil && strings.Contains(body.Query, "conversionReport"):
		return m.conversions(appID, body.Query)
	case err == nil && strings.Contains(body.Query, "productOfferV2"):
		return m.offers(body.Query)
	}
	return parseError()
}

func parseError() (*http.Response, error) {
	return answer(http.StatusBadRequest, []byte(`{"errors":[{"message":"error [10010]: request parsing error","extensions":{"code":10010,"message":"request parsing error"}}]}`))
}

// link imitates generateShortLink: the same AppID, page and subIds always
// give the same short link.
func (m *Mock) link(appID, query string) (*http.Response, error) {
	a := reLink.FindStringSubmatch(query)
	if a == nil {
		return parseError()
	}
	var origin string
	var subIDs []string
	if json.Unmarshal([]byte(a[1]), &origin) != nil || json.Unmarshal([]byte(a[2]), &subIDs) != nil ||
		!strings.Contains(origin, "shopee.com.br") {
		return answer(http.StatusOK, []byte(`{"errors":[{"message":"error [11001]: invalid originUrl","extensions":{"code":11001,"message":"invalid originUrl"}}]}`))
	}
	m.recordLink(appID, origin, subIDs)
	h := fnv.New64a()
	_, _ = h.Write([]byte(appID + "|" + origin + "|" + strings.Join(subIDs, ",")))
	code := strconv.FormatUint(h.Sum64(), 36)
	out, err := json.Marshal(map[string]any{"data": map[string]any{"generateShortLink": map[string]any{
		"shortLink": "https://s.shopee.com.br/" + code,
	}}})
	if err != nil {
		return nil, err
	}
	return answer(http.StatusOK, out)
}

func (m *Mock) offers(query string) (*http.Response, error) {
	args := map[string]int64{"page": 1, "limit": 20, "sortType": 1}
	for _, a := range reArg.FindAllStringSubmatch(query, -1) {
		args[a[1]], _ = strconv.ParseInt(a[2], 10, 64)
	}

	var rec struct {
		Data struct {
			ProductOfferV2 struct {
				Nodes []map[string]any `json:"nodes"`
			} `json:"productOfferV2"`
		} `json:"data"`
	}
	b, err := testdata.ReadFile("testdata/product_offer_v2.json")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&rec); err != nil {
		return nil, err
	}

	days := 0.0
	if m.Evolve {
		days = max(0, m.Now().Sub(recordedAt).Hours()/24)
	}
	var nodes []map[string]any
	for _, n := range rec.Data.ProductOfferV2.Nodes {
		if cat := args["productCatId"]; cat > 0 && !hasCategory(n, cat) {
			continue
		}
		if item := args["itemId"]; item > 0 && stringOf(n["itemId"]) != strconv.FormatInt(item, 10) {
			continue
		}
		sales, _ := n["sales"].(json.Number).Int64()
		n["sales"] = sales + int64(days*dailyPace(n["itemId"].(json.Number).String(), sales))
		nodes = append(nodes, n)
	}

	number := func(n map[string]any, field string) float64 {
		v, _ := strconv.ParseFloat(strings.Trim(stringOf(n[field]), `"`), 64)
		return v
	}
	switch args["sortType"] {
	case int64(SortBestSellers):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(number(b, "sales"), number(a, "sales")) })
	case int64(SortCommission):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int {
			return cmp.Compare(number(b, "commissionRate"), number(a, "commissionRate"))
		})
	case int64(SortPriceDesc):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(number(b, "priceMin"), number(a, "priceMin")) })
	case int64(SortPriceAsc):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(number(a, "priceMin"), number(b, "priceMin")) })
	}

	limit := min(max(args["limit"], 1), PageLimit)
	start := min((max(args["page"], 1)-1)*limit, int64(len(nodes)))
	end := min(start+limit, int64(len(nodes)))
	page := nodes[start:end]
	if page == nil {
		page = []map[string]any{}
	}
	out, err := json.Marshal(map[string]any{"data": map[string]any{"productOfferV2": map[string]any{
		"nodes":    page,
		"pageInfo": map[string]any{"page": args["page"], "limit": limit, "hasNextPage": end < int64(len(nodes))},
	}}})
	if err != nil {
		return nil, err
	}
	return answer(http.StatusOK, out)
}

// dailyPace gives each item a steady sales growth per day: most grow slowly,
// some take off.
func dailyPace(itemID string, sales int64) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(itemID))
	x := h.Sum32()
	pace := float64(sales) * float64(x%40) / 2000 // up to 2% a day
	if x%7 == 0 {
		pace = pace*6 + 150
	}
	return pace
}

func hasCategory(n map[string]any, cat int64) bool {
	ids, _ := n["productCatIds"].([]any)
	for _, id := range ids {
		if v, _ := id.(json.Number).Int64(); v == cat {
			return true
		}
	}
	return false
}

func stringOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

func recorded(name string) (*http.Response, error) {
	b, err := testdata.ReadFile("testdata/" + name)
	if err != nil {
		return nil, err
	}
	return answer(http.StatusOK, b)
}

func answer(status int, body []byte) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

var reItemURL = regexp.MustCompile(`(?:-i\.\d+\.|/product/\d+/)(\d+)`)

func (m *Mock) recordLink(appID, origin string, subIDs []string) {
	a := reItemURL.FindStringSubmatch(origin)
	if a == nil {
		return
	}
	item, _ := strconv.ParseInt(a[1], 10, 64)
	l := generatedLink{itemID: item, subID: strings.Join(subIDs, "-")}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.links == nil {
		m.links = map[string][]generatedLink{}
	}
	if !slices.Contains(m.links[appID], l) {
		m.links[appID] = append(m.links[appID], l)
	}
}

var (
	reReportArg = regexp.MustCompile(`(purchaseTimeStart|purchaseTimeEnd|limit):(\d+)`)
	reScrollID  = regexp.MustCompile(`scrollId:"mock-(\d+)"`)
)

// conversions imitates conversionReport: 0 to 3 conversions a day, always the
// same for the same AppID and day. Most come from the links the AppID
// generated (with their subIds); the rest, from catalog products without a
// subId. The order status moves on with age: unpaid, pending and completed,
// with a few cancelled.
func (m *Mock) conversions(appID, query string) (*http.Response, error) {
	args := map[string]int64{"limit": ReportLimit}
	for _, a := range reReportArg.FindAllStringSubmatch(query, -1) {
		args[a[1]], _ = strconv.ParseInt(a[2], 10, 64)
	}
	offset := int64(0)
	if a := reScrollID.FindStringSubmatch(query); a != nil {
		offset, _ = strconv.ParseInt(a[1], 10, 64)
	}

	var rec struct {
		Data struct {
			ProductOfferV2 struct {
				Nodes []struct {
					ItemID         json.Number `json:"itemId"`
					ProductName    string      `json:"productName"`
					ShopName       string      `json:"shopName"`
					PriceMin       string      `json:"priceMin"`
					CommissionRate string      `json:"commissionRate"`
				} `json:"nodes"`
			} `json:"productOfferV2"`
		} `json:"data"`
	}
	b, err := testdata.ReadFile("testdata/product_offer_v2.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	nodes := rec.Data.ProductOfferV2.Nodes
	byItem := map[string]int{}
	for i, n := range nodes {
		byItem[n.ItemID.String()] = i
	}
	m.mu.Lock()
	links := slices.Clone(m.links[appID])
	m.mu.Unlock()

	now := m.Now().Unix()
	from, to := args["purchaseTimeStart"], min(args["purchaseTimeEnd"], now)
	var all []map[string]any
	for day := from - from%86400; day <= to; day += 86400 {
		for j := range int(hash(appID, day, -1) % 4) {
			h := hash(appID, day, j)
			purchase := day + int64(h%86400)
			if purchase < from || purchase > to {
				continue
			}
			idx, sub := int((h>>8)%uint64(len(nodes))), ""
			if len(links) > 0 && h%10 < 7 {
				l := links[int((h>>16)%uint64(len(links)))]
				if i, ok := byItem[strconv.FormatInt(l.itemID, 10)]; ok {
					idx, sub = i, l.subID
				}
			}
			n := nodes[idx]
			qty := int64(1)
			if h%5 == 0 {
				qty = 2
			}
			price, _ := decimal(n.PriceMin).scale(2)
			rate, _ := decimal(n.CommissionRate).scale(4)
			commission := (price*qty*rate + 5000) / 10000

			age := now - purchase
			status := "COMPLETED"
			switch {
			case h%11 == 0:
				status = "CANCELLED"
			case age < 2*86400:
				status = "UNPAID"
			case age < 15*86400:
				status = "PENDING"
			}
			if status == "CANCELLED" {
				commission = 0
			}
			id := int64(h>>1) & (1<<52 - 1)
			all = append(all, map[string]any{
				"purchaseTime": purchase,
				"clickTime":    purchase - int64(h%7200),
				"conversionId": id,
				"utmContent":   sub,
				"orders": []any{map[string]any{
					"orderId":     strconv.FormatInt(id%1_000_000_000_000, 10),
					"orderStatus": status,
					"items": []any{map[string]any{
						"itemId":              n.ItemID,
						"itemName":            n.ProductName,
						"shopName":            n.ShopName,
						"modelId":             0,
						"itemPrice":           n.PriceMin,
						"qty":                 qty,
						"itemTotalCommission": centsText(commission),
					}},
				}},
			})
		}
	}

	limit := min(max(args["limit"], 1), ReportLimit)
	offset = min(offset, int64(len(all)))
	end := min(offset+limit, int64(len(all)))
	page := all[offset:end]
	if page == nil {
		page = []map[string]any{}
	}
	scroll := ""
	if end < int64(len(all)) {
		scroll = "mock-" + strconv.FormatInt(end, 10)
	}
	out, err := json.Marshal(map[string]any{"data": map[string]any{"conversionReport": map[string]any{
		"nodes":    page,
		"pageInfo": map[string]any{"limit": limit, "hasNextPage": scroll != "", "scrollId": scroll},
	}}})
	if err != nil {
		return nil, err
	}
	return answer(http.StatusOK, out)
}

// centsText writes non-negative cents as Shopee does ("12.34").
func centsText(cents int64) string {
	frac := strconv.FormatInt(cents%100, 10)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return strconv.FormatInt(cents/100, 10) + "." + frac
}

func hash(appID string, day int64, j int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(appID + "|" + strconv.FormatInt(day, 10) + "|" + strconv.Itoa(j)))
	return h.Sum64()
}
