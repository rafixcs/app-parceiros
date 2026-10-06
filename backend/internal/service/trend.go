package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	// ActiveWindow: products not collected for longer than this leave the
	// radar.
	ActiveWindow = 48 * time.Hour

	DefaultPerPage     = 24
	MaxPerPage         = 50
	maxQuery           = 100
	defaultHistoryDays = 30
	maxHistoryDays     = 90
)

// trendProducts is what the radar needs from the catalog (ProductService).
type trendProducts interface {
	ForTrends(ctx context.Context, since time.Time) ([]domain.ProductBaseline, error)
	Product(ctx context.Context, id uuid.UUID) (domain.Product, error)
	History(ctx context.Context, id uuid.UUID, since time.Time) ([]domain.Snapshot, error)
	CategoryNames(ctx context.Context, source domain.Source, ids []int64) (map[int64]string, error)
}

// TrendService computes the trend score of the products and serves the radar.
type TrendService struct {
	repo     domain.TrendRepository
	products trendProducts
	now      func() time.Time
}

func NewTrendService(repo domain.TrendRepository, products domain.ProductRepository) *TrendService {
	return &TrendService{repo: repo, products: products, now: time.Now}
}

// Compute recomputes the score of every product collected in the last
// ActiveWindow and drops from the radar those that were not (worker).
func (s *TrendService) Compute(ctx context.Context) (int, error) {
	now := s.now()
	current, err := s.products.ForTrends(ctx, now.Add(-ActiveWindow))
	if err != nil {
		return 0, fmt.Errorf("reading products: %w", err)
	}
	growth := make([]*int64, len(current))
	raw := make([]float64, len(current))
	for i, c := range current {
		growth[i] = domain.SalesGrowth7d(domain.TrendInput{
			Sales: c.Sales, CollectedAt: c.CollectedAt,
			BaseSales: c.BaseSales, BaseCollectedAt: c.BaseCollectedAt,
			CommissionBP: c.CommissionBP, Rating: c.Rating,
		})
		raw[i] = domain.RawScore(growth[i], c.CommissionBP, c.Rating)
	}
	scores := domain.NormalizeScores(raw)

	ts := make([]domain.Trend, len(current))
	for i, c := range current {
		ts[i] = domain.TrendOfProduct(c.Product)
		ts[i].Score = scores[i]
		ts[i].SalesGrowth7d = growth[i]
	}
	return len(ts), s.repo.Replace(ctx, now, ts)
}

// Radar lists the radar with filters and sorting.
func (s *TrendService) Radar(ctx context.Context, f domain.RadarFilter) (domain.RadarPage, error) {
	f, pattern, err := validRadarFilter(f)
	if err != nil {
		return domain.RadarPage{}, err
	}
	items, total, err := s.repo.Radar(ctx, f, pattern)
	if err != nil {
		return domain.RadarPage{}, err
	}
	updatedAt, err := s.repo.UpdatedAt(ctx)
	if err != nil {
		return domain.RadarPage{}, err
	}
	if items == nil {
		items = []domain.Trend{}
	}
	return domain.RadarPage{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage, UpdatedAt: updatedAt}, nil
}

// validRadarFilter checks the filter, fills the defaults and returns the LIKE
// pattern of the query.
func validRadarFilter(f domain.RadarFilter) (domain.RadarFilter, string, error) {
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PerPage == 0 {
		f.PerPage = DefaultPerPage
	}
	switch {
	case f.Page < 1 || f.Page > 1000:
		return f, "", domain.ErrInvalidPage
	case f.PerPage < 1 || f.PerPage > MaxPerPage:
		return f, "", domain.ErrInvalidPerPage
	case f.MinPrice != nil && *f.MinPrice < 0, f.MaxPrice != nil && *f.MaxPrice < 0:
		return f, "", domain.ErrNegativePrice
	case f.MinCommission != nil && (*f.MinCommission < 0 || *f.MinCommission > 10000):
		return f, "", domain.ErrInvalidMinCommission
	case f.MinRating != nil && (*f.MinRating < 0 || *f.MinRating > 5):
		return f, "", domain.ErrInvalidMinRating
	}
	switch f.Sort {
	case "":
		f.Sort = domain.SortTrend
	case domain.SortTrend, domain.SortCommission, domain.SortEarnings, domain.SortSales:
	default:
		return f, "", domain.ErrInvalidSort
	}
	f.Query = strings.TrimSpace(f.Query)
	if f.Query == "" {
		return f, "", nil
	}
	if len([]rune(f.Query)) > maxQuery {
		return f, "", domain.ErrQueryTooLong
	}
	return f, "%" + escapeLike(f.Query) + "%", nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Product returns the radar item with the history of the last `days`. A
// product that left the radar still shows, without a score.
func (s *TrendService) Product(ctx context.Context, id uuid.UUID, days int) (domain.ProductDetail, error) {
	if days == 0 {
		days = defaultHistoryDays
	}
	if days < 1 || days > maxHistoryDays {
		return domain.ProductDetail{}, domain.ErrInvalidHistoryDays
	}
	var out domain.ProductDetail
	t, err := s.repo.Trend(ctx, id)
	switch {
	case err == nil:
		out.Product = t
	case errors.Is(err, domain.ErrNotFound):
		p, err := s.products.Product(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ProductDetail{}, domain.ErrProductNotFound
		}
		if err != nil {
			return domain.ProductDetail{}, err
		}
		out.Product = domain.TrendOfProduct(p)
	default:
		return domain.ProductDetail{}, err
	}
	out.History, err = s.products.History(ctx, id, s.now().Add(-time.Duration(days)*24*time.Hour))
	return out, err
}

// Categories lists the level 1 categories present in the radar, with their
// names when known.
func (s *TrendService) Categories(ctx context.Context) ([]domain.RadarCategory, error) {
	cs, err := s.repo.Categories(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	names, err := s.products.CategoryNames(ctx, domain.SourceShopee, ids)
	if err != nil {
		return nil, err
	}
	for i, c := range cs {
		name, ok := names[c.ID]
		if !ok {
			name = fmt.Sprintf("Categoria %d", c.ID)
		}
		cs[i].Name = name
	}
	if cs == nil {
		cs = []domain.RadarCategory{}
	}
	return cs, nil
}

// SetClock replaces the clock (tests).
func (s *TrendService) SetClock(now func() time.Time) { s.now = now }
