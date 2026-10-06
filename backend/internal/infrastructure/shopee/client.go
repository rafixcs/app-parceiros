// Package shopee is the client of the Shopee Brazil affiliate Open API
// (GraphQL signed with SHA-256). It uses only the official API.
//
// Without an approved credential, use NewMock: the same client, with the
// answers recorded in testdata/ served by an http.RoundTripper.
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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ratelimit"
)

const (
	DefaultURL = "https://open-api.affiliate.shopee.com.br/graphql"
	// PageLimit is the most items productOfferV2 returns per page.
	PageLimit = 50
)

// Credential is the AppID and Secret of an affiliate account. Its String and
// GoString hide the Secret.
type Credential = domain.ShopeeCredential

// SortType is the order of productOfferV2.
type SortType int

const (
	SortRelevance   SortType = 1
	SortBestSellers SortType = 2
	SortPriceDesc   SortType = 3
	SortPriceAsc    SortType = 4
	SortCommission  SortType = 5
)

type Config struct {
	URL     string
	HTTP    *http.Client
	Limiter ratelimit.Limiter
	// MaxWait is how long a call accepts to wait for the rate limit before
	// returning domain.ErrSourceLimit.
	MaxWait time.Duration
	Now     func() time.Time
}

type Client struct {
	url     string
	http    *http.Client
	limiter ratelimit.Limiter
	maxWait time.Duration
	now     func() time.Time
}

var _ domain.ShopeeCredentialValidator = (*Client)(nil)

func NewClient(c Config) *Client {
	if c.URL == "" {
		c.URL = DefaultURL
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if c.Limiter == nil {
		c.Limiter = ratelimit.Unlimited{}
	}
	if c.MaxWait == 0 {
		c.MaxWait = 20 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Client{url: c.URL, http: c.HTTP, limiter: c.Limiter, maxWait: c.MaxWait, now: c.Now}
}

type OfferFilter struct {
	CategoryID int64 // 0 = all
	Sort       SortType
	Page       int
	Limit      int
}

// Offers queries productOfferV2. Pages must be asked in sequence, inside the
// same job.
func (c *Client) Offers(ctx context.Context, cred Credential, f OfferFilter) (domain.CatalogPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > PageLimit {
		f.Limit = PageLimit
	}
	if f.Sort == 0 {
		f.Sort = SortBestSellers
	}
	args := []string{
		"sortType:" + strconv.Itoa(int(f.Sort)),
		"page:" + strconv.Itoa(f.Page),
		"limit:" + strconv.Itoa(f.Limit),
	}
	if f.CategoryID > 0 {
		args = append([]string{"productCatId:" + strconv.FormatInt(f.CategoryID, 10)}, args...)
	}
	query := "{productOfferV2(" + strings.Join(args, ",") + "){nodes{" + offerFields + "} pageInfo{page limit hasNextPage}}}"

	raw, err := c.call(ctx, cred, query)
	if err != nil {
		return domain.CatalogPage{}, err
	}
	var resp struct {
		Data struct {
			ProductOfferV2 struct {
				Nodes    []offer `json:"nodes"`
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"productOfferV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return domain.CatalogPage{}, fmt.Errorf("%w: invalid productOfferV2 answer: %v", domain.ErrSourceUnavailable, err)
	}
	out := domain.CatalogPage{
		HasNext: resp.Data.ProductOfferV2.PageInfo.HasNextPage,
		Raw:     raw,
		Offers:  make([]domain.Offer, 0, len(resp.Data.ProductOfferV2.Nodes)),
	}
	for _, n := range resp.Data.ProductOfferV2.Nodes {
		o, err := n.normalize()
		if err != nil {
			return domain.CatalogPage{}, fmt.Errorf("%w: item %s: %v", domain.ErrSourceUnavailable, n.ItemID, err)
		}
		out.Offers = append(out.Offers, o)
	}
	return out, nil
}

// OfferByItem looks a product up by its itemId in productOfferV2. It returns
// domain.ErrSourceNotFound when Shopee does not offer it in the affiliate
// program.
func (c *Client) OfferByItem(ctx context.Context, cred Credential, itemID int64) (domain.Offer, error) {
	if itemID <= 0 {
		return domain.Offer{}, domain.ErrSourceNotFound
	}
	query := "{productOfferV2(itemId:" + strconv.FormatInt(itemID, 10) + ",page:1,limit:1){nodes{" + offerFields + "} pageInfo{page limit hasNextPage}}}"
	raw, err := c.call(ctx, cred, query)
	if err != nil {
		return domain.Offer{}, err
	}
	var resp struct {
		Data struct {
			ProductOfferV2 struct {
				Nodes []offer `json:"nodes"`
			} `json:"productOfferV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return domain.Offer{}, fmt.Errorf("%w: invalid productOfferV2 answer: %v", domain.ErrSourceUnavailable, err)
	}
	for _, n := range resp.Data.ProductOfferV2.Nodes {
		o, err := n.normalize()
		if err != nil {
			return domain.Offer{}, fmt.Errorf("%w: item %s: %v", domain.ErrSourceUnavailable, n.ItemID, err)
		}
		if o.ItemID == itemID {
			return o, nil
		}
	}
	return domain.Offer{}, domain.ErrSourceNotFound
}

// MaxSubIDs is how many subIds generateShortLink accepts.
const MaxSubIDs = 5

var reSubID = regexp.MustCompile(`^[A-Za-z0-9]{1,50}$`)

// GenerateLink calls generateShortLink: it returns the short affiliate link
// of the credential to the page `origin`, tagged with the (alphanumeric)
// subIds.
func (c *Client) GenerateLink(ctx context.Context, cred Credential, origin string, subIDs []string) (string, error) {
	if len(subIDs) > MaxSubIDs {
		return "", fmt.Errorf("at most %d subIds", MaxSubIDs)
	}
	for _, s := range subIDs {
		if !reSubID.MatchString(s) {
			return "", fmt.Errorf("invalid subId %q", s)
		}
	}
	// GraphQL strings use the same escaping as JSON.
	url, err := json.Marshal(origin)
	if err != nil {
		return "", err
	}
	subs, err := json.Marshal(subIDs)
	if err != nil {
		return "", err
	}
	if subIDs == nil {
		subs = []byte("[]")
	}
	query := "mutation{generateShortLink(input:{originUrl:" + string(url) + ",subIds:" + string(subs) + "}){shortLink}}"
	raw, err := c.call(ctx, cred, query)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data struct {
			GenerateShortLink struct {
				ShortLink string `json:"shortLink"`
			} `json:"generateShortLink"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Data.GenerateShortLink.ShortLink == "" {
		return "", fmt.Errorf("%w: generateShortLink answer without shortLink", domain.ErrSourceUnavailable)
	}
	return resp.Data.GenerateShortLink.ShortLink, nil
}

// Validate makes a test call with the credential. It returns nil when Shopee
// accepts it, or domain.ErrSourceInvalidCredential, ErrSourceAccessDenied,
// ErrSourceLimit or ErrSourceUnavailable.
func (c *Client) Validate(ctx context.Context, cred Credential) error {
	_, err := c.call(ctx, cred, "{productOfferV2(page:1,limit:1){nodes{itemId}}}")
	return err
}

// offerFields are the fields asked of productOfferV2. The offerLink is not
// asked: it carries the affiliate id of the credential that queried (the
// app's), and each user's link comes from generateShortLink with their own
// credential.
const offerFields = "itemId productName shopId shopName imageUrl productLink priceMin priceMax commissionRate sales ratingStar productCatIds"

func (c *Client) call(ctx context.Context, cred Credential, query string) ([]byte, error) {
	if err := ratelimit.Wait(ctx, c.limiter, "shopee:"+cred.AppID, c.maxWait); err != nil {
		var lim *ratelimit.ErrLimited
		if errors.As(err, &lim) {
			return nil, fmt.Errorf("%w: %v", domain.ErrSourceLimit, err)
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
	req.Header.Set("Authorization", Sign(cred, c.now(), payload))

	res, err := c.http.Do(req)
	if err != nil {
		// The transport error carries the URL, never the header with the
		// signature.
		return nil, fmt.Errorf("%w: %v", domain.ErrSourceUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: reading answer: %v", domain.ErrSourceUnavailable, err)
	}

	var env struct {
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code int `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(body, &env)
	if len(env.Errors) > 0 {
		e := env.Errors[0]
		return nil, &APIError{Code: e.Extensions.Code, Message: e.Message}
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP status %d", domain.ErrSourceUnavailable, res.StatusCode)
	}
	return body, nil
}

// Sign builds the Authorization header:
// SHA256 Credential={AppId}, Timestamp={ts}, Signature=sha256(AppId+ts+payload+Secret).
func Sign(cred Credential, now time.Time, payload []byte) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	h := sha256.New()
	h.Write([]byte(cred.AppID))
	h.Write([]byte(ts))
	h.Write(payload)
	h.Write([]byte(cred.Secret))
	return "SHA256 Credential=" + cred.AppID + ", Timestamp=" + ts + ", Signature=" + hex.EncodeToString(h.Sum(nil))
}

// APIError is an error returned by Shopee in the GraphQL errors field.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("shopee: error %d: %s", e.Code, e.Message) }

// Unwrap maps the code to the source errors of the domain.
func (e *APIError) Unwrap() error {
	switch e.Code {
	case 10020:
		return domain.ErrSourceInvalidCredential
	case 10030:
		return domain.ErrSourceLimit
	case 10031, 10032, 10033, 10034, 10035:
		return domain.ErrSourceAccessDenied
	default:
		return domain.ErrSourceUnavailable
	}
}

// offer is a node of productOfferV2. Prices, rates and ratings come as
// decimal text (sometimes as numbers); hence the decimal type.
type offer struct {
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

func (n offer) normalize() (domain.Offer, error) {
	var o domain.Offer
	var err error
	if o.ItemID, err = n.ItemID.scale(0); err != nil || o.ItemID <= 0 {
		return o, errors.New("invalid itemId")
	}
	if o.ShopID, err = n.ShopID.scale(0); err != nil {
		return o, fmt.Errorf("shopId: %w", err)
	}
	if o.MinPriceCents, err = n.PriceMin.scale(2); err != nil {
		return o, fmt.Errorf("priceMin: %w", err)
	}
	if o.MaxPriceCents, err = n.PriceMax.scale(2); err != nil {
		return o, fmt.Errorf("priceMax: %w", err)
	}
	if o.MaxPriceCents < o.MinPriceCents {
		o.MaxPriceCents = o.MinPriceCents
	}
	// commissionRate is a fraction (0.12 = 12%); in basis points, × 10,000.
	bp, err := n.CommissionRate.scale(4)
	if err != nil || bp < 0 || bp > 10000 {
		return o, errors.New("invalid commissionRate")
	}
	o.CommissionBP = int32(bp)
	if o.Sales, err = n.Sales.scale(0); err != nil {
		return o, fmt.Errorf("sales: %w", err)
	}
	if rating, err := n.RatingStar.scale(2); err == nil && rating > 0 && rating <= 500 {
		v := float64(rating) / 100
		o.Rating = &v
	}
	for _, c := range n.ProductCatIDs {
		if id, err := c.scale(0); err == nil && id > 0 {
			o.Categories = append(o.Categories, id)
		}
	}
	o.Name = strings.TrimSpace(n.ProductName)
	o.ShopName = strings.TrimSpace(n.ShopName)
	o.ImageURL = n.ImageURL
	o.URL = n.ProductLink
	return o, nil
}
