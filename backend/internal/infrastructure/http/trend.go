package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// TrendService is what the radar handler needs (service.TrendService).
type TrendService interface {
	Radar(ctx context.Context, f domain.RadarFilter) (domain.RadarPage, error)
	Categories(ctx context.Context) ([]domain.RadarCategory, error)
	Product(ctx context.Context, id uuid.UUID, days int) (domain.ProductDetail, error)
}

// TrendHandler serves the radar of products in high demand.
type TrendHandler struct {
	svc TrendService
	log *slog.Logger
}

func NewTrendHandler(svc TrendService, log *slog.Logger) *TrendHandler {
	return &TrendHandler{svc: svc, log: log}
}

// Routes registers the radar under /v1/workspaces/{workspaceId}.
func (h *TrendHandler) Routes() Routes {
	return Routes{Workspace: func(r chi.Router) {
		r.Get("/radar", h.radar)
		r.Get("/radar/categories", h.categories)
		r.Get("/radar/products/{productId}", h.product)
	}}
}

func (h *TrendHandler) radar(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	f := domain.RadarFilter{Query: q.Get("q"), Sort: domain.RadarSort(q.Get("sort"))}
	ok := true
	parse := func(name string, fn func(string) error) {
		if v := q.Get(name); v != "" && ok && fn(v) != nil {
			ok = false
		}
	}
	parse("category", func(v string) error { return parseInt64(v, &f.Category) })
	parse("min_price", func(v string) error { return parseInt64(v, &f.MinPrice) })
	parse("max_price", func(v string) error { return parseInt64(v, &f.MaxPrice) })
	parse("min_commission", func(v string) error {
		n, err := strconv.ParseInt(v, 10, 32)
		c := int32(n)
		f.MinCommission = &c
		return err
	})
	parse("min_rating", func(v string) error {
		n, err := strconv.ParseFloat(v, 64)
		f.MinRating = &n
		return err
	})
	parse("page", func(v string) (err error) { f.Page, err = strconv.Atoi(v); return err })
	parse("per_page", func(v string) (err error) { f.PerPage, err = strconv.Atoi(v); return err })
	if !ok {
		invalidRequest(w)
		return
	}
	p, err := h.svc.Radar(r.Context(), f)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, radarPageResponse{
		Items: mapSlice(p.Items, trendResponseOf), Total: p.Total, Page: p.Page, PerPage: p.PerPage,
		UpdatedAt: p.UpdatedAt,
	})
}

func (h *TrendHandler) categories(w nethttp.ResponseWriter, r *nethttp.Request) {
	cs, err := h.svc.Categories(r.Context())
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(cs, func(c domain.RadarCategory) radarCategoryResponse {
		return radarCategoryResponse{ID: c.ID, Name: c.Name, Products: c.Products}
	}))
}

func (h *TrendHandler) product(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "productId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrProductNotFound)
		return
	}
	days := 0
	if v := r.URL.Query().Get("days"); v != "" {
		if days, err = strconv.Atoi(v); err != nil {
			invalidRequest(w)
			return
		}
	}
	d, err := h.svc.Product(r.Context(), id, days)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, productDetailResponse{
		Product: trendResponseOf(d.Product), History: mapSlice(d.History, snapshotResponseOf),
	})
}

func parseInt64(v string, dst **int64) error {
	n, err := strconv.ParseInt(v, 10, 64)
	*dst = &n
	return err
}
