// Package shopee é o cliente da Open API de afiliados da Shopee Brasil
// (GraphQL com assinatura SHA-256). Só usa a API oficial.
//
// Sem credencial aprovada, use NovoMock: o mesmo cliente, com as respostas
// gravadas em testdata/ servidas por um http.RoundTripper.
package shopee

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/ratelimit"
)

const (
	URLPadrao = "https://open-api.affiliate.shopee.com.br/graphql"
	// LimitePagina é o máximo de itens que o productOfferV2 devolve por página.
	LimitePagina = 50
)

// Credencial é o AppID e o Secret de uma conta de afiliado. O Secret nunca
// vai para log, erro ou resposta: String e GoString escondem o valor.
type Credencial struct {
	AppID  string
	Secret string
}

func (c Credencial) String() string {
	return "shopee.Credencial{AppID:" + c.AppID + ", Secret:[oculto]}"
}
func (c Credencial) GoString() string { return c.String() }

// Ordenação do productOfferV2.
type Ordem int

const (
	OrdemRelevancia   Ordem = 1
	OrdemMaisVendidos Ordem = 2
	OrdemMaiorPreco   Ordem = 3
	OrdemMenorPreco   Ordem = 4
	OrdemComissao     Ordem = 5
)

type Config struct {
	URL       string
	HTTP      *http.Client
	Limitador ratelimit.Limitador
	// EsperaMax é quanto uma chamada aceita esperar pelo rate limit antes de
	// devolver fontes.ErrLimite.
	EsperaMax time.Duration
	Agora     func() time.Time
}

type Cliente struct {
	url       string
	http      *http.Client
	limitador ratelimit.Limitador
	esperaMax time.Duration
	agora     func() time.Time
}

func NovoCliente(c Config) *Cliente {
	if c.URL == "" {
		c.URL = URLPadrao
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if c.Limitador == nil {
		c.Limitador = ratelimit.Livre{}
	}
	if c.EsperaMax == 0 {
		c.EsperaMax = 20 * time.Second
	}
	if c.Agora == nil {
		c.Agora = time.Now
	}
	return &Cliente{url: c.URL, http: c.HTTP, limitador: c.Limitador, esperaMax: c.EsperaMax, agora: c.Agora}
}

type FiltroOfertas struct {
	CategoriaID int64 // 0 = todas
	Ordem       Ordem
	Pagina      int
	Limite      int
}

// Ofertas consulta o productOfferV2. As páginas devem ser pedidas em
// sequência, dentro do mesmo job.
func (c *Cliente) Ofertas(ctx context.Context, cred Credencial, f FiltroOfertas) (fontes.PaginaCatalogo, error) {
	if f.Pagina < 1 {
		f.Pagina = 1
	}
	if f.Limite < 1 || f.Limite > LimitePagina {
		f.Limite = LimitePagina
	}
	if f.Ordem == 0 {
		f.Ordem = OrdemMaisVendidos
	}
	args := []string{
		"sortType:" + strconv.Itoa(int(f.Ordem)),
		"page:" + strconv.Itoa(f.Pagina),
		"limit:" + strconv.Itoa(f.Limite),
	}
	if f.CategoriaID > 0 {
		args = append([]string{"productCatId:" + strconv.FormatInt(f.CategoriaID, 10)}, args...)
	}
	query := "{productOfferV2(" + strings.Join(args, ",") + "){nodes{" + camposOferta + "} pageInfo{page limit hasNextPage}}}"

	bruto, err := c.chamar(ctx, cred, query)
	if err != nil {
		return fontes.PaginaCatalogo{}, err
	}
	var resp struct {
		Data struct {
			ProductOfferV2 struct {
				Nodes    []oferta `json:"nodes"`
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"productOfferV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bruto, &resp); err != nil {
		return fontes.PaginaCatalogo{}, fmt.Errorf("%w: resposta do productOfferV2 inválida: %v", fontes.ErrIndisponivel, err)
	}
	out := fontes.PaginaCatalogo{
		TemProxima: resp.Data.ProductOfferV2.PageInfo.HasNextPage,
		Bruto:      bruto,
		Ofertas:    make([]fontes.Oferta, 0, len(resp.Data.ProductOfferV2.Nodes)),
	}
	for _, n := range resp.Data.ProductOfferV2.Nodes {
		o, err := n.normalizar()
		if err != nil {
			return fontes.PaginaCatalogo{}, fmt.Errorf("%w: item %s: %v", fontes.ErrIndisponivel, n.ItemID, err)
		}
		out.Ofertas = append(out.Ofertas, o)
	}
	return out, nil
}

// Validar faz uma chamada de teste com a credencial. Devolve nil se a Shopee
// aceitou, ou fontes.ErrCredencialInvalida, ErrAcessoNegado, ErrLimite ou
// ErrIndisponivel.
func (c *Cliente) Validar(ctx context.Context, cred Credencial) error {
	_, err := c.chamar(ctx, cred, "{productOfferV2(page:1,limit:1){nodes{itemId}}}")
	return err
}

// camposOferta são os campos pedidos ao productOfferV2. O offerLink não é
// pedido: ele carrega o ID de afiliado da credencial que consultou (a do app),
// e o link de cada usuário sai do generateShortLink com a credencial dele.
const camposOferta = "itemId productName shopId shopName imageUrl productLink priceMin priceMax commissionRate sales ratingStar productCatIds"

func (c *Cliente) chamar(ctx context.Context, cred Credencial, query string) ([]byte, error) {
	if err := ratelimit.Esperar(ctx, c.limitador, "shopee:"+cred.AppID, c.esperaMax); err != nil {
		var lim *ratelimit.ErrLimite
		if errors.As(err, &lim) {
			return nil, fmt.Errorf("%w: %v", fontes.ErrLimite, err)
		}
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", Assinar(cred, c.agora(), payload))

	res, err := c.http.Do(req)
	if err != nil {
		// O erro de transporte traz a URL, nunca o cabeçalho com a assinatura.
		return nil, fmt.Errorf("%w: %v", fontes.ErrIndisponivel, err)
	}
	defer func() { _ = res.Body.Close() }()
	corpo, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: lendo resposta: %v", fontes.ErrIndisponivel, err)
	}

	var env struct {
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code int `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(corpo, &env)
	if len(env.Errors) > 0 {
		e := env.Errors[0]
		return nil, &ErroAPI{Codigo: e.Extensions.Code, Mensagem: e.Message}
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status HTTP %d", fontes.ErrIndisponivel, res.StatusCode)
	}
	return corpo, nil
}

// Assinar monta o cabeçalho Authorization:
// SHA256 Credential={AppId}, Timestamp={ts}, Signature=sha256(AppId+ts+payload+Secret).
func Assinar(cred Credencial, agora time.Time, payload []byte) string {
	ts := strconv.FormatInt(agora.Unix(), 10)
	h := sha256.New()
	h.Write([]byte(cred.AppID))
	h.Write([]byte(ts))
	h.Write(payload)
	h.Write([]byte(cred.Secret))
	return "SHA256 Credential=" + cred.AppID + ", Timestamp=" + ts + ", Signature=" + hex.EncodeToString(h.Sum(nil))
}

// ErroAPI é um erro devolvido pela Shopee no campo errors do GraphQL.
type ErroAPI struct {
	Codigo   int
	Mensagem string
}

func (e *ErroAPI) Error() string { return fmt.Sprintf("shopee: erro %d: %s", e.Codigo, e.Mensagem) }

// Unwrap traduz o código para os erros comuns de fontes.
func (e *ErroAPI) Unwrap() error {
	switch e.Codigo {
	case 10020:
		return fontes.ErrCredencialInvalida
	case 10030:
		return fontes.ErrLimite
	case 10031, 10032, 10033, 10034, 10035:
		return fontes.ErrAcessoNegado
	default:
		return fontes.ErrIndisponivel
	}
}

// oferta é um nó do productOfferV2. Preços, taxas e notas chegam como texto
// decimal (às vezes como número); por isso o tipo decimal.
type oferta struct {
	ItemID         decimal   `json:"itemId"`
	ProductName    string    `json:"productName"`
	ShopID         decimal   `json:"shopId"`
	ShopName       string    `json:"shopName"`
	ImageURL       string    `json:"imageUrl"`
	ProductLink    string    `json:"productLink"`
	PriceMin       decimal   `json:"priceMin"`
	PriceMax       decimal   `json:"priceMax"`
	CommissionRate decimal   `json:"commissionRate"`
	Sales          decimal   `json:"sales"`
	RatingStar     decimal   `json:"ratingStar"`
	ProductCatIDs  []decimal `json:"productCatIds"`
}

func (n oferta) normalizar() (fontes.Oferta, error) {
	var o fontes.Oferta
	var err error
	if o.ItemID, err = n.ItemID.escalar(0); err != nil || o.ItemID <= 0 {
		return o, fmt.Errorf("itemId inválido")
	}
	if o.LojaID, err = n.ShopID.escalar(0); err != nil {
		return o, fmt.Errorf("shopId: %w", err)
	}
	if o.PrecoMinCentavos, err = n.PriceMin.escalar(2); err != nil {
		return o, fmt.Errorf("priceMin: %w", err)
	}
	if o.PrecoMaxCentavos, err = n.PriceMax.escalar(2); err != nil {
		return o, fmt.Errorf("priceMax: %w", err)
	}
	if o.PrecoMaxCentavos < o.PrecoMinCentavos {
		o.PrecoMaxCentavos = o.PrecoMinCentavos
	}
	// commissionRate é uma fração (0.12 = 12%); em basis points, × 10.000.
	bp, err := n.CommissionRate.escalar(4)
	if err != nil || bp < 0 || bp > 10000 {
		return o, fmt.Errorf("commissionRate inválido")
	}
	o.ComissaoBP = int32(bp)
	if o.Vendas, err = n.Sales.escalar(0); err != nil {
		return o, fmt.Errorf("sales: %w", err)
	}
	if nota, err := n.RatingStar.escalar(2); err == nil && nota > 0 && nota <= 500 {
		v := float64(nota) / 100
		o.Nota = &v
	}
	for _, c := range n.ProductCatIDs {
		if id, err := c.escalar(0); err == nil && id > 0 {
			o.Categorias = append(o.Categorias, id)
		}
	}
	o.Nome = strings.TrimSpace(n.ProductName)
	o.LojaNome = strings.TrimSpace(n.ShopName)
	o.ImagemURL = n.ImageURL
	o.URL = n.ProductLink
	return o, nil
}
