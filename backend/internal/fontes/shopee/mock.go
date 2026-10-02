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
)

//go:embed testdata/*.json
var testdata embed.FS

// AppIDs que o mock trata como erro, com o código da Shopee no próprio número.
const (
	MockAppIDInvalido = "10020" // assinatura recusada
	MockAppIDLimite   = "10030" // rate limit da Shopee
	MockAppIDNegado   = "10031" // acesso negado
)

// Mock imita a Open API da Shopee a partir das respostas gravadas em
// testdata/. Qualquer AppID numérico é aceito, exceto os MockAppID*. Para
// conferir a assinatura de um AppID, registre o Secret em Segredos.
//
// Com Evoluir, as vendas crescem a cada dia desde a data da gravação, num
// ritmo próprio de cada item, para o radar ter tendência no ambiente local.
type Mock struct {
	Segredos map[string]string
	Evoluir  bool
	Agora    func() time.Time

	mu       sync.Mutex
	chamadas int
}

// Data da gravação de testdata/product_offer_v2.json.
var dataGravacao = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// NovoMock devolve um Cliente que fala com o Mock em vez da Shopee.
func NovoMock(m *Mock, c Config) *Cliente {
	if m.Agora == nil {
		m.Agora = time.Now
	}
	c.URL = "https://mock.shopee.invalid/graphql"
	c.HTTP = &http.Client{Transport: m}
	return NovoCliente(c)
}

// Chamadas conta as requisições recebidas.
func (m *Mock) Chamadas() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chamadas
}

// Categorias devolve as categorias de nível 1 do catálogo gravado.
func Categorias() ([]struct {
	ID   int64  `json:"id"`
	Nome string `json:"nome"`
}, error) {
	var out []struct {
		ID   int64  `json:"id"`
		Nome string `json:"nome"`
	}
	b, err := testdata.ReadFile("testdata/categorias.json")
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(b, &out)
}

var (
	reAuth = regexp.MustCompile(`^SHA256 Credential=(\d+), Timestamp=(\d+), Signature=([0-9a-f]{64})$`)
	reArg  = regexp.MustCompile(`(productCatId|itemId|sortType|page|limit):(\d+)`)
	// originUrl:"...",subIds:[...] do generateShortLink.
	reLink = regexp.MustCompile(`originUrl:("(?:[^"\\]|\\.)*"),subIds:(\[[^\]]*\])`)
)

func (m *Mock) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.chamadas++
	m.mu.Unlock()

	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	auth := reAuth.FindStringSubmatch(req.Header.Get("Authorization"))
	if auth == nil {
		return gravada("erro_10020.json")
	}
	appID := auth[1]
	switch appID {
	case MockAppIDInvalido:
		return gravada("erro_10020.json")
	case MockAppIDLimite:
		return gravada("erro_10030.json")
	case MockAppIDNegado:
		return gravada("erro_10031.json")
	}
	if secret, ok := m.Segredos[appID]; ok {
		ts, _ := strconv.ParseInt(auth[2], 10, 64)
		if Assinar(Credencial{AppID: appID, Secret: secret}, time.Unix(ts, 0), payload) != req.Header.Get("Authorization") {
			return gravada("erro_10020.json")
		}
	}

	var corpo struct {
		Query string `json:"query"`
	}
	err = json.Unmarshal(payload, &corpo)
	switch {
	case err == nil && strings.Contains(corpo.Query, "generateShortLink"):
		return m.link(appID, corpo.Query)
	case err == nil && strings.Contains(corpo.Query, "productOfferV2"):
		return m.ofertas(corpo.Query)
	}
	return erroParse()
}

func erroParse() (*http.Response, error) {
	return resposta(http.StatusBadRequest, []byte(`{"errors":[{"message":"error [10010]: request parsing error","extensions":{"code":10010,"message":"request parsing error"}}]}`))
}

// link imita o generateShortLink: o mesmo AppID, página e subIds dão sempre
// o mesmo link curto.
func (m *Mock) link(appID, query string) (*http.Response, error) {
	a := reLink.FindStringSubmatch(query)
	if a == nil {
		return erroParse()
	}
	var origem string
	var subIDs []string
	if json.Unmarshal([]byte(a[1]), &origem) != nil || json.Unmarshal([]byte(a[2]), &subIDs) != nil ||
		!strings.Contains(origem, "shopee.com.br") {
		return resposta(http.StatusOK, []byte(`{"errors":[{"message":"error [11001]: invalid originUrl","extensions":{"code":11001,"message":"invalid originUrl"}}]}`))
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(appID + "|" + origem + "|" + strings.Join(subIDs, ",")))
	codigo := strconv.FormatUint(h.Sum64(), 36)
	out, err := json.Marshal(map[string]any{"data": map[string]any{"generateShortLink": map[string]any{
		"shortLink": "https://s.shopee.com.br/" + codigo,
	}}})
	if err != nil {
		return nil, err
	}
	return resposta(http.StatusOK, out)
}

func (m *Mock) ofertas(query string) (*http.Response, error) {
	args := map[string]int64{"page": 1, "limit": 20, "sortType": 1}
	for _, a := range reArg.FindAllStringSubmatch(query, -1) {
		args[a[1]], _ = strconv.ParseInt(a[2], 10, 64)
	}

	var gravacao struct {
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
	if err := dec.Decode(&gravacao); err != nil {
		return nil, err
	}

	dias := 0.0
	if m.Evoluir {
		dias = max(0, m.Agora().Sub(dataGravacao).Hours()/24)
	}
	var nodes []map[string]any
	for _, n := range gravacao.Data.ProductOfferV2.Nodes {
		if cat := args["productCatId"]; cat > 0 && !temCategoria(n, cat) {
			continue
		}
		if item := args["itemId"]; item > 0 && stringDe(n["itemId"]) != strconv.FormatInt(item, 10) {
			continue
		}
		vendas, _ := n["sales"].(json.Number).Int64()
		n["sales"] = vendas + int64(dias*ritmoDiario(n["itemId"].(json.Number).String(), vendas))
		nodes = append(nodes, n)
	}

	numero := func(n map[string]any, campo string) float64 {
		v, _ := strconv.ParseFloat(strings.Trim(stringDe(n[campo]), `"`), 64)
		return v
	}
	switch args["sortType"] {
	case int64(OrdemMaisVendidos):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(numero(b, "sales"), numero(a, "sales")) })
	case int64(OrdemComissao):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int {
			return cmp.Compare(numero(b, "commissionRate"), numero(a, "commissionRate"))
		})
	case int64(OrdemMaiorPreco):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(numero(b, "priceMin"), numero(a, "priceMin")) })
	case int64(OrdemMenorPreco):
		slices.SortStableFunc(nodes, func(a, b map[string]any) int { return cmp.Compare(numero(a, "priceMin"), numero(b, "priceMin")) })
	}

	limite := min(max(args["limit"], 1), LimitePagina)
	inicio := min((max(args["page"], 1)-1)*limite, int64(len(nodes)))
	fim := min(inicio+limite, int64(len(nodes)))
	pagina := nodes[inicio:fim]
	if pagina == nil {
		pagina = []map[string]any{}
	}
	out, err := json.Marshal(map[string]any{"data": map[string]any{"productOfferV2": map[string]any{
		"nodes":    pagina,
		"pageInfo": map[string]any{"page": args["page"], "limit": limite, "hasNextPage": fim < int64(len(nodes))},
	}}})
	if err != nil {
		return nil, err
	}
	return resposta(http.StatusOK, out)
}

// ritmoDiario dá a cada item um crescimento de vendas por dia estável: a
// maioria cresce devagar, alguns disparam.
func ritmoDiario(itemID string, vendas int64) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(itemID))
	x := h.Sum32()
	ritmo := float64(vendas) * float64(x%40) / 2000 // até 2% ao dia
	if x%7 == 0 {
		ritmo = ritmo*6 + 150
	}
	return ritmo
}

func temCategoria(n map[string]any, cat int64) bool {
	ids, _ := n["productCatIds"].([]any)
	for _, id := range ids {
		if v, _ := id.(json.Number).Int64(); v == cat {
			return true
		}
	}
	return false
}

func stringDe(v any) string {
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

func gravada(nome string) (*http.Response, error) {
	b, err := testdata.ReadFile("testdata/" + nome)
	if err != nil {
		return nil, err
	}
	return resposta(http.StatusOK, b)
}

func resposta(status int, corpo []byte) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(corpo)),
	}, nil
}
