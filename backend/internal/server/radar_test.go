package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

type countingTrendQueue struct{ n int }

func (q *countingTrendQueue) EnqueueTrends(context.Context) error { q.n++; return nil }

type trendJSON struct {
	ProductID            uuid.UUID `json:"product_id"`
	Name                 string    `json:"name"`
	Categories           []int64   `json:"categories"`
	MinPriceCents        int64     `json:"min_price_cents"`
	CommissionBP         int32     `json:"commission_bp"`
	EarningsPerSaleCents int64     `json:"earnings_per_sale_cents"`
	Sales                int64     `json:"sales"`
	Rating               *float64  `json:"rating"`
	Score                float64   `json:"score"`
	SalesGrowth7d        *int64    `json:"sales_growth_7d"`
}

type radarPageJSON struct {
	Items     []trendJSON `json:"items"`
	Total     int64       `json:"total"`
	UpdatedAt *time.Time  `json:"updated_at"`
}

// TestRadar runs the radar end to end with the Shopee mock: two collections
// 7 days apart, the trend computation and the radar routes.
func TestRadar(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Hour)
	clock := func() time.Time { return now }
	raw := &storage.Memory{}
	trendQueue := &countingTrendQueue{}
	a := newTestApp(t, func(in *infra) {
		client := shopee.NewMock(&shopee.Mock{Evolve: true, Now: clock}, shopee.Config{})
		in.shopee = client
		in.catalog = shopee.AppCatalog{Client: client, Credential: shopee.Credential{AppID: "1", Secret: "x"}}
		in.raw = raw
		in.trendQueue = trendQueue
	})
	products, trends := a.svcs.products, a.svcs.trends
	products.SetClock(clock)
	trends.SetClock(clock)

	cats, err := shopee.Categories()
	if err != nil {
		t.Fatal(err)
	}
	if err := products.SaveCategories(ctx, domain.SourceShopee, cats); err != nil {
		t.Fatal(err)
	}
	if monitored, err := products.MonitoredCategories(ctx, domain.SourceShopee); err != nil || len(monitored) != len(cats) {
		t.Fatalf("monitored: %v %v", monitored, err)
	}

	collect := func() {
		t.Helper()
		if _, err := products.Snapshot(ctx, 0, 10); err != nil {
			t.Fatal(err)
		}
	}
	collect()
	if n, err := trends.Compute(ctx); err != nil || n != 80 {
		t.Fatalf("first computation: %d %v", n, err)
	}
	now = now.Add(7 * 24 * time.Hour)
	collect()
	if n, err := trends.Compute(ctx); err != nil || n != 80 {
		t.Fatalf("second computation: %d %v", n, err)
	}
	if trendQueue.n != 2 {
		t.Fatalf("trends enqueued %d times", trendQueue.n)
	}
	if len(raw.Keys()) != 4 { // 80 items = 2 pages per collection
		t.Fatalf("raw answers kept: %v", raw.Keys())
	}

	ws := a.personal("ana")
	base := wsPath(ws.ID, "/radar")

	var p radarPageJSON
	a.must("ana", http.MethodGet, base, nil, &p, http.StatusOK)
	if p.Total != 80 || len(p.Items) != service.DefaultPerPage || p.UpdatedAt == nil || !p.UpdatedAt.Equal(now) {
		t.Fatalf("radar: total %d, items %d, updated %v", p.Total, len(p.Items), p.UpdatedAt)
	}
	if p.Items[0].Score != 100 || p.Items[0].SalesGrowth7d == nil || *p.Items[0].SalesGrowth7d <= 0 {
		t.Fatalf("first item: %+v", p.Items[0])
	}
	for i := 1; i < len(p.Items); i++ {
		if p.Items[i].Score > p.Items[i-1].Score {
			t.Fatal("radar out of trend order")
		}
	}
	it := p.Items[0]
	if it.EarningsPerSaleCents != domain.EarningsPerSale(it.MinPriceCents, it.CommissionBP) {
		t.Fatalf("earnings per sale: %+v", it)
	}

	t.Run("sorting", func(t *testing.T) {
		field := map[string]func(trendJSON) int64{
			"commission": func(i trendJSON) int64 { return int64(i.CommissionBP) },
			"earnings":   func(i trendJSON) int64 { return i.EarningsPerSaleCents },
			"sales":      func(i trendJSON) int64 { return i.Sales },
		}
		for sort, f := range field {
			var p radarPageJSON
			a.must("ana", http.MethodGet, base+"?per_page=50&sort="+sort, nil, &p, http.StatusOK)
			for i := 1; i < len(p.Items); i++ {
				if f(p.Items[i]) > f(p.Items[i-1]) {
					t.Fatalf("%s out of order at %d", sort, i)
				}
			}
		}
	})

	t.Run("filters", func(t *testing.T) {
		var p radarPageJSON
		q := url.Values{"category": {"100004"}, "min_commission": {"700"}, "min_rating": {"4.5"},
			"min_price": {"2000"}, "max_price": {"20000"}, "per_page": {"50"}}
		a.must("ana", http.MethodGet, base+"?"+q.Encode(), nil, &p, http.StatusOK)
		if p.Total == 0 {
			t.Fatal("filter with no result; adjust the case")
		}
		for _, i := range p.Items {
			if i.Categories[0] != 100004 || i.CommissionBP < 700 || i.Rating == nil || *i.Rating < 4.5 ||
				i.MinPriceCents < 2000 || i.MinPriceCents > 20000 {
				t.Fatalf("item outside the filter: %+v", i)
			}
		}

		a.must("ana", http.MethodGet, base+"?q="+url.QueryEscape("fone bluetooth"), nil, &p, http.StatusOK)
		if p.Total != 1 || !strings.Contains(p.Items[0].Name, "Fone bluetooth") {
			t.Fatalf("search: %d %+v", p.Total, p.Items)
		}
		a.must("ana", http.MethodGet, base+"?q="+url.QueryEscape("100%_"), nil, &p, http.StatusOK)
		if p.Total != 0 {
			t.Fatalf("wildcard in the search: %d", p.Total)
		}
		a.must("ana", http.MethodGet, base+"?page=4&per_page=24", nil, &p, http.StatusOK)
		if len(p.Items) != 8 || p.Total != 80 {
			t.Fatalf("last page: %d items", len(p.Items))
		}

		for bad, code := range map[string]string{
			"sort=price": "invalid_sort", "per_page=51": "invalid_per_page", "min_rating=6": "invalid_min_rating",
			"min_price=-1": "negative_price", "q=" + strings.Repeat("a", 101): "query_too_long",
		} {
			a.mustFail("ana", http.MethodGet, base+"?"+bad, nil, http.StatusUnprocessableEntity, code)
		}
		a.mustFail("ana", http.MethodGet, base+"?min_commission=x", nil, http.StatusBadRequest, "invalid_request")
	})

	t.Run("categories", func(t *testing.T) {
		var cs []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Products int64  `json:"products"`
		}
		a.must("ana", http.MethodGet, base+"/categories", nil, &cs, http.StatusOK)
		if len(cs) != 5 || cs[0].Products != 16 || cs[0].Name == "" || strings.HasPrefix(cs[0].Name, "Categoria ") {
			t.Fatalf("%+v", cs)
		}
	})

	t.Run("detail with history", func(t *testing.T) {
		var d struct {
			Product trendJSON `json:"product"`
			History []struct {
				Sales int64 `json:"sales"`
			} `json:"history"`
		}
		a.must("ana", http.MethodGet, base+"/products/"+it.ProductID.String()+"?days=90", nil, &d, http.StatusOK)
		if d.Product.ProductID != it.ProductID || len(d.History) != 2 || d.History[1].Sales <= d.History[0].Sales {
			t.Fatalf("%+v", d)
		}
		a.mustFail("ana", http.MethodGet, base+"/products/"+uuid.Nil.String(), nil, http.StatusNotFound, "product_not_found")
		a.mustFail("ana", http.MethodGet, base+"/products/"+it.ProductID.String()+"?days=91", nil,
			http.StatusUnprocessableEntity, "invalid_history_days")
	})

	t.Run("members of the workspace only", func(t *testing.T) {
		for _, path := range []string{base, base + "/categories", base + "/products/" + it.ProductID.String()} {
			a.mustFail("bia", http.MethodGet, path, nil, http.StatusNotFound, "workspace_not_found")
		}
		if st := a.call("", http.MethodGet, base, nil, nil); st != http.StatusUnauthorized {
			t.Fatalf("signed out: %d", st)
		}
	})

	t.Run("a product not collected recently leaves the radar", func(t *testing.T) {
		now = now.Add(3 * 24 * time.Hour)
		if _, err := trends.Compute(ctx); err != nil {
			t.Fatal(err)
		}
		var p radarPageJSON
		a.must("ana", http.MethodGet, base, nil, &p, http.StatusOK)
		if p.Total != 0 {
			t.Fatalf("radar with %d old products", p.Total)
		}
		var d struct {
			Product trendJSON `json:"product"`
		}
		a.must("ana", http.MethodGet, base+"/products/"+it.ProductID.String(), nil, &d, http.StatusOK)
		if d.Product.Score != 0 || d.Product.ProductID != it.ProductID {
			t.Fatalf("detail out of the radar: %+v", d.Product)
		}
	})
}
