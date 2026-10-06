package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
)

// collInCatalog is how many of the test offers are recorded in the catalog
// before the tests. The others serve to paste the link of a product not
// collected yet.
const collInCatalog = 10

// collOffers are the products the fake source knows.
func collOffers() []domain.Offer {
	out := make([]domain.Offer, 20)
	for i := range out {
		item := int64(5000 + i)
		out[i] = domain.Offer{
			ItemID: item, ShopID: 77, ShopName: "Loja Teste", Name: "Produto " + strconv.Itoa(i),
			URL:           "https://shopee.com.br/product/77/" + strconv.FormatInt(item, 10),
			Categories:    []int64{100},
			MinPriceCents: int64(1000 + 100*i), MaxPriceCents: int64(2000 + 100*i), CommissionBP: int32(500 + 10*i),
			Sales: int64(10 * i),
		}
	}
	return out
}

// collCatalog is the source with the app credential.
type collCatalog struct{ offers []domain.Offer }

func (c collCatalog) Source() domain.Source { return domain.SourceShopee }

func (c collCatalog) Offers(context.Context, domain.CatalogFilter) (domain.CatalogPage, error) {
	return domain.CatalogPage{}, nil
}

func (c collCatalog) OfferByItem(_ context.Context, itemID int64) (domain.Offer, error) {
	for _, o := range c.offers {
		if o.ItemID == itemID {
			return o, nil
		}
	}
	return domain.Offer{}, domain.ErrSourceNotFound
}

// collAffiliator generates a distinct short link per user, page and subIds.
type collAffiliator struct {
	mu        sync.Mutex
	connected map[uuid.UUID]bool
	refuse    bool // the source refuses the credential
}

func (f *collAffiliator) connect(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected[id] = true
}

func (f *collAffiliator) Connected(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected[id], nil
}

func (f *collAffiliator) GenerateLink(_ context.Context, id uuid.UUID, origin string, subIDs []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case !f.connected[id]:
		return "", domain.ErrNoCredential
	case f.refuse:
		return "", domain.ErrSourceInvalidCredential
	}
	h := sha256.Sum256([]byte(id.String() + "|" + origin + "|" + strings.Join(subIDs, ",")))
	return "https://s.shopee.com.br/" + hex.EncodeToString(h[:6]), nil
}

// collLinkParser understands https://shopee.com.br/Name-i.<shop>.<item>,
// shopee.com.br/product/<shop>/<item> and the short links s.shopee.com.br.
type collLinkParser struct{}

var (
	collNamePath    = regexp.MustCompile(`-i\.(\d+)\.(\d+)$`)
	collProductPath = regexp.MustCompile(`^/product/(\d+)/(\d+)/?$`)
)

func (collLinkParser) ParseProductLink(raw string) (domain.ProductLink, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return domain.ProductLink{}, domain.ErrInvalidProductLink
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if host == "s.shopee.com.br" {
		return domain.ProductLink{}, domain.ErrShortLink
	}
	if host != "shopee.com.br" {
		return domain.ProductLink{}, domain.ErrInvalidProductLink
	}
	m := collNamePath.FindStringSubmatch(u.Path)
	if m == nil {
		m = collProductPath.FindStringSubmatch(u.Path)
	}
	if m == nil {
		return domain.ProductLink{}, domain.ErrInvalidProductLink
	}
	shop, _ := strconv.ParseInt(m[1], 10, 64)
	item, _ := strconv.ParseInt(m[2], 10, 64)
	return domain.ProductLink{Source: domain.SourceShopee, ShopID: shop, ItemID: item}, nil
}

// collQueue keeps the enqueued jobs; process runs the worker on them.
type collQueue struct {
	mu   sync.Mutex
	jobs []domain.AffiliateLinkJob
}

func (q *collQueue) EnqueueAffiliateLinks(_ context.Context, jobs ...domain.AffiliateLinkJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, jobs...)
	return nil
}

func (q *collQueue) take() []domain.AffiliateLinkJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.jobs
	q.jobs = nil
	return out
}

type collEnv struct {
	*testApp
	queue      *collQueue
	affiliator *collAffiliator
	offers     []domain.Offer
}

// newCollEnv builds the app with the fakes. Without catalog, the server has
// no app credential and imports no pasted product.
func newCollEnv(t *testing.T, withCatalog bool) *collEnv {
	t.Helper()
	e := &collEnv{
		queue:      &collQueue{},
		affiliator: &collAffiliator{connected: map[uuid.UUID]bool{}},
		offers:     collOffers(),
	}
	e.testApp = newTestApp(t, func(in *infra) {
		in.affiliator = e.affiliator
		in.linkParser = collLinkParser{}
		in.linkQueue = e.queue
		if withCatalog {
			in.catalog = collCatalog{offers: e.offers}
		} else {
			in.catalog = nil
		}
	})
	if err := repository.NewPostgresProduct(e.pool).Record(context.Background(), domain.SourceShopee, time.Now(), e.offers[:collInCatalog]); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *collEnv) connect(sub string) { e.affiliator.connect(e.me(sub).ID) }

func (e *collEnv) productID(i int) uuid.UUID {
	e.t.Helper()
	p, err := e.svcs.products.ProductByItem(context.Background(), domain.SourceShopee, e.offers[i].ItemID)
	if err != nil {
		e.t.Fatal(err)
	}
	return p.ID
}

// process runs the generate_affiliate_link worker on each enqueued job.
func (e *collEnv) process() int {
	e.t.Helper()
	w := &queue.GenerateAffiliateLinkWorker{Svc: e.svcs.collections, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	jobs := e.queue.take()
	for _, j := range jobs {
		job := &river.Job[queue.GenerateAffiliateLinkArgs]{
			JobRow: &rivertype.JobRow{Kind: "generate_affiliate_link", Attempt: 1, MaxAttempts: 6},
			Args:   queue.GenerateAffiliateLinkArgs{ItemID: j.ItemID, WorkspaceID: j.WorkspaceID, UserID: j.UserID},
		}
		if err := w.Work(context.Background(), job); err != nil {
			e.t.Fatalf("generate_affiliate_link: %v", err)
		}
	}
	return len(jobs)
}

func (e *collEnv) save(sub string, ws uuid.UUID, body any) collItemJSON {
	e.t.Helper()
	var it collItemJSON
	e.must(sub, http.MethodPost, wsPath(ws, "/items"), body, &it, http.StatusCreated)
	return it
}

type collProductJSON struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	CommissionBP         int32     `json:"commission_bp"`
	EarningsPerSaleCents int64     `json:"earnings_per_sale_cents"`
}

type collLinkJSON struct {
	Channel string `json:"channel"`
	SubID   string `json:"sub_id"`
	URL     string `json:"url"`
}

type collItemJSON struct {
	ID            uuid.UUID       `json:"id"`
	Product       collProductJSON `json:"product"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Notes         string          `json:"notes"`
	Tags          []string        `json:"tags"`
	Status        string          `json:"status"`
	AffiliateLink *string         `json:"affiliate_link"`
	LinkOrigin    string          `json:"link_origin"`
	LinkStatus    string          `json:"link_status"`
	Links         []collLinkJSON  `json:"links"`
	CollectionIDs []uuid.UUID     `json:"collection_ids"`
}

type collPageJSON struct {
	Items   []collItemJSON `json:"items"`
	Total   int64          `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}

type collCollectionJSON struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Items int64     `json:"items"`
}

func TestCollectionSaveAndGenerateLink(t *testing.T) {
	a := newCollEnv(t, true)
	ws := a.personal("ana").ID

	// Without a credential: saved with the link pending, nothing enqueued.
	it := a.save("ana", ws, map[string]any{"product_id": a.productID(0)})
	if it.LinkStatus != "pending" || it.AffiliateLink != nil || it.LinkOrigin != "auto" {
		t.Fatalf("without credential: %+v", it)
	}
	if it.Title != a.offers[0].Name || it.Status != "testing" || it.Product.Name != a.offers[0].Name {
		t.Fatalf("new item: %+v", it)
	}
	if want := domain.EarningsPerSale(a.offers[0].MinPriceCents, a.offers[0].CommissionBP); it.Product.EarningsPerSaleCents != want {
		t.Fatalf("earnings per sale %d, want %d", it.Product.EarningsPerSaleCents, want)
	}
	if n := a.process(); n != 0 {
		t.Fatalf("enqueued %d jobs without credential", n)
	}
	a.mustFail("ana", http.MethodPost, wsPath(ws, "/items/pending-links"), nil, http.StatusConflict, "no_credential")

	// Saving again returns the same item.
	var again collItemJSON
	a.must("ana", http.MethodPost, wsPath(ws, "/items"), map[string]any{"product_id": a.productID(0)}, &again, http.StatusOK)
	if again.ID != it.ID {
		t.Fatalf("saved twice: %s and %s", it.ID, again.ID)
	}

	// Once connected, the pending ones are generated.
	a.connect("ana")
	var r struct {
		Enqueued int `json:"enqueued"`
	}
	a.must("ana", http.MethodPost, wsPath(ws, "/items/pending-links"), nil, &r, http.StatusAccepted)
	if r.Enqueued != 1 || a.process() != 1 {
		t.Fatalf("pending enqueued: %+v", r)
	}
	a.must("ana", http.MethodGet, wsPath(ws, "/items/"+it.ID.String()), nil, &it, http.StatusOK)
	if it.LinkStatus != "ready" || it.AffiliateLink == nil || len(it.Links) != len(domain.Channels) {
		t.Fatalf("after generating: %+v", it)
	}
	urls := map[string]bool{}
	for _, l := range it.Links {
		if !strings.HasPrefix(l.URL, "https://s.shopee.com.br/") || l.SubID != l.Channel+"-"+domain.WorkspaceMark(ws) {
			t.Fatalf("link of channel %s: %+v", l.Channel, l)
		}
		if l.Channel == "other" && l.URL != *it.AffiliateLink {
			t.Fatalf("main link %q, channel other %q", *it.AffiliateLink, l.URL)
		}
		urls[l.URL] = true
	}
	if len(urls) != len(domain.Channels) {
		t.Fatalf("channels with the same link: %+v", it.Links)
	}

	// With a credential, a new item is born generating.
	other := a.save("ana", ws, map[string]any{"product_id": a.productID(1)})
	if other.LinkStatus != "generating" || a.process() != 1 {
		t.Fatalf("with credential: %+v", other)
	}

	var ids []uuid.UUID
	a.must("ana", http.MethodGet, wsPath(ws, "/items/products"), nil, &ids, http.StatusOK)
	if len(ids) != 2 {
		t.Fatalf("saved products: %v", ids)
	}

	// The source starts refusing the credential: the link stays pending.
	a.affiliator.mu.Lock()
	a.affiliator.refuse = true
	a.affiliator.mu.Unlock()
	third := a.save("ana", ws, map[string]any{"product_id": a.productID(2)})
	a.process()
	a.must("ana", http.MethodGet, wsPath(ws, "/items/"+third.ID.String()), nil, &third, http.StatusOK)
	if third.LinkStatus != "pending" {
		t.Fatalf("with refused credential: %+v", third)
	}

	a.must("ana", http.MethodDelete, wsPath(ws, "/items/"+other.ID.String()), nil, nil, http.StatusNoContent)
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items/"+other.ID.String()), nil, http.StatusNotFound, "item_not_found")
	a.mustFail("ana", http.MethodDelete, wsPath(ws, "/items/"+other.ID.String()), nil, http.StatusNotFound, "item_not_found")
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items/not-a-uuid"), nil, http.StatusNotFound, "item_not_found")
	a.mustFail("ana", http.MethodPost, wsPath(ws, "/items"), map[string]any{"product_id": uuid.New()}, http.StatusNotFound, "product_not_found")
	a.mustFail("ana", http.MethodPost, wsPath(ws, "/items"), map[string]any{}, http.StatusUnprocessableEntity, "product_or_link_required")
}

func TestCollectionPasteLink(t *testing.T) {
	a := newCollEnv(t, true)
	ws := a.personal("ana").ID
	link := func(o domain.Offer) string {
		return "https://shopee.com.br/Produto-Qualquer-i." + strconv.FormatInt(o.ShopID, 10) + "." + strconv.FormatInt(o.ItemID, 10) + "?sp_atk=abc"
	}

	// Product already in the catalog.
	it := a.save("ana", ws, map[string]any{"url": link(a.offers[0])})
	if it.Product.ID != a.productID(0) {
		t.Fatalf("link of a product in the catalog: %+v", it.Product)
	}

	// Product outside the catalog: fetched from the source and imported.
	outside := a.offers[collInCatalog+5]
	it = a.save("ana", ws, map[string]any{"url": "shopee.com.br/product/" + strconv.FormatInt(outside.ShopID, 10) + "/" + strconv.FormatInt(outside.ItemID, 10)})
	if it.Product.Name != outside.Name || it.Product.CommissionBP != outside.CommissionBP {
		t.Fatalf("imported product: %+v", it.Product)
	}
	if _, err := a.svcs.products.ProductByItem(context.Background(), domain.SourceShopee, outside.ItemID); err != nil {
		t.Fatalf("imported product is not in the catalog: %v", err)
	}

	items := wsPath(ws, "/items")
	a.mustFail("ana", http.MethodPost, items, map[string]any{"url": "https://s.shopee.com.br/abc123"}, http.StatusUnprocessableEntity, "short_link")
	a.mustFail("ana", http.MethodPost, items, map[string]any{"url": "https://example.com/i.1.2"}, http.StatusUnprocessableEntity, "invalid_product_link")
	a.mustFail("ana", http.MethodPost, items, map[string]any{"url": "https://shopee.com.br/Nada-i.1.999"}, http.StatusNotFound, "product_not_found")
	a.mustFail("ana", http.MethodPost, items,
		map[string]any{"url": link(outside), "product_id": a.productID(0)}, http.StatusUnprocessableEntity, "product_or_link_required")

	// Without the app credential, only links of products in the catalog work.
	b := newCollEnv(t, false)
	wsB := b.personal("bia").ID
	b.save("bia", wsB, map[string]any{"url": link(b.offers[1])})
	b.mustFail("bia", http.MethodPost, wsPath(wsB, "/items"), map[string]any{"url": link(b.offers[collInCatalog+1])},
		http.StatusServiceUnavailable, "import_unavailable")
}

func TestCollectionEditItem(t *testing.T) {
	a := newCollEnv(t, true)
	a.connect("ana")
	ws := a.personal("ana").ID
	it := a.save("ana", ws, map[string]any{"product_id": a.productID(0)})
	a.process()
	path := wsPath(ws, "/items/"+it.ID.String())

	a.must("ana", http.MethodPatch, path, map[string]any{
		"title": "  Fone que vende muito  ", "description": "Bateria de 30 h", "notes": "Testar no reels",
		"tags": []string{"fone", " Fone ", "áudio  bom", ""}, "status": "winner",
	}, &it, http.StatusOK)
	if it.Title != "Fone que vende muito" || it.Description != "Bateria de 30 h" || it.Notes != "Testar no reels" ||
		it.Status != "winner" || strings.Join(it.Tags, "|") != "fone|áudio bom" {
		t.Fatalf("after editing: %+v", it)
	}
	// Absent fields stay; an empty text clears.
	a.must("ana", http.MethodPatch, path, map[string]any{"notes": ""}, &it, http.StatusOK)
	if it.Notes != "" || it.Title != "Fone que vende muito" || len(it.Tags) != 2 {
		t.Fatalf("partial edit: %+v", it)
	}

	a.mustFail("ana", http.MethodPatch, path, map[string]any{"status": "sold"}, http.StatusUnprocessableEntity, "invalid_item_status")
	a.mustFail("ana", http.MethodPatch, path, map[string]any{"title": strings.Repeat("a", 201)}, http.StatusUnprocessableEntity, "invalid_item_title")
	a.mustFail("ana", http.MethodPatch, path, map[string]any{"tags": []string{strings.Repeat("t", 31)}}, http.StatusUnprocessableEntity, "invalid_tag")
	a.mustFail("ana", http.MethodPatch, path, map[string]any{"affiliate_link": "http://insecure.com"}, http.StatusUnprocessableEntity, "invalid_affiliate_link")
	a.mustFail("ana", http.MethodPatch, path, map[string]any{"affiliate_link": 12}, http.StatusUnprocessableEntity, "invalid_affiliate_link")
	a.mustFail("ana", http.MethodPatch, path, map[string]any{"field": 1}, http.StatusBadRequest, "invalid_json")

	// A manual link beats the automatic one, and the job does not overwrite it.
	manual := "https://s.shopee.com.br/mylink"
	a.must("ana", http.MethodPatch, path, map[string]any{"affiliate_link": manual}, &it, http.StatusOK)
	if it.LinkOrigin != "manual" || it.AffiliateLink == nil || *it.AffiliateLink != manual || it.LinkStatus != "ready" {
		t.Fatalf("manual link: %+v", it)
	}
	_ = a.queue.EnqueueAffiliateLinks(context.Background(), domain.AffiliateLinkJob{ItemID: it.ID, WorkspaceID: ws, UserID: a.me("ana").ID})
	a.process()
	a.must("ana", http.MethodGet, path, nil, &it, http.StatusOK)
	if *it.AffiliateLink != manual {
		t.Fatalf("the job overwrote the manual link: %+v", it)
	}

	// null goes back to the automatic link.
	a.must("ana", http.MethodPatch, path, map[string]any{"affiliate_link": nil}, &it, http.StatusOK)
	if it.LinkOrigin != "auto" || it.LinkStatus != "generating" || it.AffiliateLink != nil || len(it.Links) != 0 {
		t.Fatalf("back to automatic: %+v", it)
	}
	if a.process() != 1 {
		t.Fatal("did not enqueue the automatic link")
	}
	a.must("ana", http.MethodPost, path+"/link", nil, &it, http.StatusAccepted)
	if it.LinkStatus != "generating" || a.process() != 1 {
		t.Fatalf("generate again: %+v", it)
	}
	a.must("ana", http.MethodGet, path, nil, &it, http.StatusOK)
	if it.LinkStatus != "ready" || len(it.Links) != len(domain.Channels) {
		t.Fatalf("after generating again: %+v", it)
	}
}

func TestCollectionsAndFilters(t *testing.T) {
	a := newCollEnv(t, true)
	ws := a.personal("ana").ID
	cols := wsPath(ws, "/collections")

	var finds, xmas collCollectionJSON
	a.must("ana", http.MethodPost, cols, map[string]any{"name": " Achados  da semana "}, &finds, http.StatusCreated)
	if finds.Name != "Achados da semana" || finds.Items != 0 {
		t.Fatalf("collection created: %+v", finds)
	}
	a.mustFail("ana", http.MethodPost, cols, map[string]any{"name": "achados DA semana"}, http.StatusConflict, "collection_exists")
	a.mustFail("ana", http.MethodPost, cols, map[string]any{"name": " "}, http.StatusUnprocessableEntity, "invalid_collection_name")
	a.must("ana", http.MethodPost, cols, map[string]any{"name": "Natal"}, &xmas, http.StatusCreated)
	a.mustFail("ana", http.MethodPatch, cols+"/"+xmas.ID.String(), map[string]any{"name": "Achados da semana"}, http.StatusConflict, "collection_exists")
	a.must("ana", http.MethodPatch, cols+"/"+xmas.ID.String(), map[string]any{"name": "Natal 2026"}, &xmas, http.StatusOK)
	if xmas.Name != "Natal 2026" {
		t.Fatalf("rename: %+v", xmas)
	}

	var items []collItemJSON
	for i := range 4 {
		items = append(items, a.save("ana", ws, map[string]any{"product_id": a.productID(i)}))
	}
	// One item in two collections; another in one.
	var it collItemJSON
	a.must("ana", http.MethodPut, wsPath(ws, "/items/"+items[0].ID.String()+"/collections"),
		map[string]any{"collection_ids": []uuid.UUID{finds.ID, xmas.ID, finds.ID}}, &it, http.StatusOK)
	if len(it.CollectionIDs) != 2 {
		t.Fatalf("collections of the item: %+v", it.CollectionIDs)
	}
	a.must("ana", http.MethodPut, wsPath(ws, "/items/"+items[1].ID.String()+"/collections"),
		map[string]any{"collection_ids": []uuid.UUID{xmas.ID}}, &it, http.StatusOK)
	a.mustFail("ana", http.MethodPut, wsPath(ws, "/items/"+items[1].ID.String()+"/collections"),
		map[string]any{"collection_ids": []uuid.UUID{uuid.New()}}, http.StatusNotFound, "collection_not_found")

	var cs []collCollectionJSON
	a.must("ana", http.MethodGet, cols, nil, &cs, http.StatusOK)
	if len(cs) != 2 || cs[0].Name != "Achados da semana" || cs[0].Items != 1 || cs[1].Items != 2 {
		t.Fatalf("collections: %+v", cs)
	}

	a.must("ana", http.MethodPatch, wsPath(ws, "/items/"+items[2].ID.String()),
		map[string]any{"title": "Garrafa térmica 100%_inox", "status": "discarded", "tags": []string{"Casa"}}, nil, http.StatusOK)

	list := func(query string) collPageJSON {
		t.Helper()
		var p collPageJSON
		a.must("ana", http.MethodGet, wsPath(ws, "/items"+query), nil, &p, http.StatusOK)
		return p
	}
	if p := list(""); p.Total != 4 || len(p.Items) != 4 || p.Items[0].ID != items[3].ID || p.Page != 1 || p.PerPage != 30 {
		t.Fatalf("all, newest first: %+v", p)
	}
	if p := list("?collection=" + xmas.ID.String()); p.Total != 2 {
		t.Fatalf("filter by collection: %+v", p)
	}
	if p := list("?status=discarded"); p.Total != 1 || p.Items[0].ID != items[2].ID {
		t.Fatalf("filter by status: %+v", p)
	}
	if p := list("?tag=Casa"); p.Total != 1 {
		t.Fatalf("filter by tag: %+v", p)
	}
	if p := list("?q=100%25_inox"); p.Total != 1 {
		t.Fatalf("search with %% and _: %+v", p)
	}
	if p := list("?q=%25"); p.Total != 1 {
		t.Fatalf("search only for %%: %+v", p)
	}
	if p := list("?per_page=3&page=2"); p.Total != 4 || len(p.Items) != 1 {
		t.Fatalf("pagination: %+v", p)
	}
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items?status=x"), nil, http.StatusUnprocessableEntity, "invalid_item_status")
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items?per_page=500"), nil, http.StatusUnprocessableEntity, "invalid_item_page")
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items?q="+strings.Repeat("a", 101)), nil, http.StatusUnprocessableEntity, "query_too_long")
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items?collection=x"), nil, http.StatusBadRequest, "invalid_request")
	a.mustFail("ana", http.MethodGet, wsPath(ws, "/items?page=x"), nil, http.StatusBadRequest, "invalid_request")

	// Deleting the collection keeps the items.
	a.must("ana", http.MethodDelete, cols+"/"+xmas.ID.String(), nil, nil, http.StatusNoContent)
	a.must("ana", http.MethodGet, wsPath(ws, "/items/"+items[1].ID.String()), nil, &it, http.StatusOK)
	if len(it.CollectionIDs) != 0 {
		t.Fatalf("item still in the deleted collection: %+v", it.CollectionIDs)
	}
	a.mustFail("ana", http.MethodDelete, cols+"/"+xmas.ID.String(), nil, http.StatusNotFound, "collection_not_found")
}

// TestCollectionImport covers the import used by curation: new items carry
// the mentor's tip, saved ones are counted, and all go to the collection.
func TestCollectionImport(t *testing.T) {
	a := newCollEnv(t, true)
	a.connect("ana")
	ws := a.personal("ana").ID
	actor := domain.Actor{UserID: a.me("ana").ID, WorkspaceID: ws}
	ctx := context.Background()

	saved := a.save("ana", ws, map[string]any{"product_id": a.productID(0)})
	a.process()
	res, err := a.svcs.collections.Import(ctx, actor, []domain.ImportedItem{
		{ProductID: a.productID(0), Comment: "já tinha"},
		{ProductID: a.productID(1), Comment: "  vende muito  "},
	}, "Lista da semana")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.AlreadySaved != 1 || res.CollectionID == nil || res.LinkStatus != domain.LinkGenerating {
		t.Fatalf("import: %+v", res)
	}
	if a.process() != 1 {
		t.Fatal("did not enqueue the link of the new item")
	}
	got, err := a.svcs.collections.ItemsOfProducts(ctx, actor, []uuid.UUID{a.productID(0), a.productID(1)})
	if err != nil {
		t.Fatal(err)
	}
	if got[a.productID(0)].ID != saved.ID || got[a.productID(0)].Notes != "" ||
		got[a.productID(1)].Notes != "Dica do mentor: vende muito" || got[a.productID(1)].LinkStatus != domain.LinkReady {
		t.Fatalf("imported items: %+v", got)
	}
	// The same name again reuses the collection.
	res, err = a.svcs.collections.Import(ctx, actor, []domain.ImportedItem{{ProductID: a.productID(1)}}, "lista DA semana")
	if err != nil || res.Created != 0 || res.AlreadySaved != 1 {
		t.Fatalf("import again: %+v %v", res, err)
	}
	var cs []collCollectionJSON
	a.must("ana", http.MethodGet, wsPath(ws, "/collections"), nil, &cs, http.StatusOK)
	if len(cs) != 1 || cs[0].Items != 2 || cs[0].ID != *res.CollectionID {
		t.Fatalf("collections after import: %+v", cs)
	}
	if _, err := a.svcs.collections.Import(ctx, actor, []domain.ImportedItem{{ProductID: uuid.New()}}, ""); err != domain.ErrProductNotFound {
		t.Fatalf("import of an unknown product: %v", err)
	}
}

// TestCollectionLeak checks that the collection belongs to the user, in one
// workspace only: neither the mentor, nor another affiliate, nor the user in
// another workspace see it, through the API or straight in the database
// (RLS).
func TestCollectionLeak(t *testing.T) {
	a := newCollEnv(t, true)
	ctx := context.Background()

	mentorship := a.mentorship("mentor", "Turma")
	ws := mentorship.ID
	ana := a.join("mentor", "ana", ws).ID
	beto := a.join("mentor", "beto", ws).ID

	a.connect("ana")
	it := a.save("ana", ws, map[string]any{"product_id": a.productID(0)})
	a.process()
	var col collCollectionJSON
	a.must("ana", http.MethodPost, wsPath(ws, "/collections"), map[string]any{"name": "Minhas"}, &col, http.StatusCreated)
	a.must("ana", http.MethodPut, wsPath(ws, "/items/"+it.ID.String()+"/collections"), map[string]any{"collection_ids": []uuid.UUID{col.ID}}, nil, http.StatusOK)
	a.must("ana", http.MethodPatch, wsPath(ws, "/items/"+it.ID.String()), map[string]any{"notes": "segredo"}, nil, http.StatusOK)

	item := wsPath(ws, "/items/"+it.ID.String())
	for _, sub := range []string{"mentor", "beto"} {
		var p collPageJSON
		a.must(sub, http.MethodGet, wsPath(ws, "/items"), nil, &p, http.StatusOK)
		var cs []collCollectionJSON
		a.must(sub, http.MethodGet, wsPath(ws, "/collections"), nil, &cs, http.StatusOK)
		var ids []uuid.UUID
		a.must(sub, http.MethodGet, wsPath(ws, "/items/products"), nil, &ids, http.StatusOK)
		if p.Total != 0 || len(cs) != 0 || len(ids) != 0 {
			t.Fatalf("%s sees ana's collection: %+v %+v %v", sub, p, cs, ids)
		}
		a.mustFail(sub, http.MethodGet, item, nil, http.StatusNotFound, "item_not_found")
		a.mustFail(sub, http.MethodPatch, item, map[string]any{"title": "x"}, http.StatusNotFound, "item_not_found")
		a.mustFail(sub, http.MethodPost, item+"/link", nil, http.StatusNotFound, "item_not_found")
		a.mustFail(sub, http.MethodPut, item+"/collections", map[string]any{"collection_ids": []uuid.UUID{}}, http.StatusNotFound, "item_not_found")
		a.mustFail(sub, http.MethodDelete, item, nil, http.StatusNotFound, "item_not_found")
		a.mustFail(sub, http.MethodPatch, wsPath(ws, "/collections/"+col.ID.String()), map[string]any{"name": "x"}, http.StatusNotFound, "collection_not_found")
		a.mustFail(sub, http.MethodDelete, wsPath(ws, "/collections/"+col.ID.String()), nil, http.StatusNotFound, "collection_not_found")
	}
	// Ana's item in the mentorship does not show in her personal workspace.
	personal := a.personal("ana").ID
	var p collPageJSON
	a.must("ana", http.MethodGet, wsPath(personal, "/items"), nil, &p, http.StatusOK)
	if p.Total != 0 {
		t.Fatalf("mentorship item shows in the personal workspace: %+v", p)
	}
	a.mustFail("ana", http.MethodGet, wsPath(personal, "/items/"+it.ID.String()), nil, http.StatusNotFound, "item_not_found")
	// Whoever is not a member does not even reach the routes.
	a.mustFail("intruder", http.MethodGet, wsPath(ws, "/items"), nil, http.StatusNotFound, "workspace_not_found")

	// Straight in the database, with the app role: the policies hide every
	// row of another user or workspace, and refuse writes in ana's name.
	count := func(s database.Scope) int {
		t.Helper()
		total := 0
		err := database.InTx(ctx, a.pool, s, func(tx pgx.Tx) error {
			for _, table := range []string{"saved_items", "collections", "collection_items", "channel_links"} {
				var n int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
					return err
				}
				total += n
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	if n := count(database.Scope{UserID: ana.String(), WorkspaceID: ws.String()}); n != 1+1+1+len(domain.Channels) {
		t.Fatalf("ana sees %d rows", n)
	}
	for name, s := range map[string]database.Scope{
		"mentor":          {UserID: a.me("mentor").ID.String(), WorkspaceID: ws.String()},
		"other affiliate": {UserID: beto.String(), WorkspaceID: ws.String()},
		"other workspace": {UserID: ana.String(), WorkspaceID: personal.String()},
		"no workspace":    {UserID: ana.String()},
	} {
		if n := count(s); n != 0 {
			t.Fatalf("%s sees %d rows of ana", name, n)
		}
	}
	err := database.InTx(ctx, a.pool, database.Scope{UserID: beto.String(), WorkspaceID: ws.String()}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO collections (workspace_id, user_id, name) VALUES ($1, $2, 'invasão')", ws, ana)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("beto wrote a collection in ana's name: %v", err)
	}
}

func TestWorkspaceMark(t *testing.T) {
	id := uuid.MustParse("0a1b2c3d-4e5f-6789-abcd-ef0123456789")
	if m := domain.WorkspaceMark(id); m != "w0a1b2c3d4e5f" || !domain.IsWorkspaceMark(m) {
		t.Fatalf("mark %q", m)
	}
	for _, s := range []string{"", "instagram", "x0a1b2c3d4e5f", "w0a1b2c3d4e5", "w0a1b2c3d4e5g"} {
		if domain.IsWorkspaceMark(s) {
			t.Fatalf("%q is not a mark", s)
		}
	}
}
