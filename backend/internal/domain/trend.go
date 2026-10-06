package domain

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
)

// TrendInput is what the score needs from a product.
type TrendInput struct {
	Sales           int64
	CollectedAt     time.Time
	BaseSales       int64
	BaseCollectedAt time.Time
	CommissionBP    int32
	Rating          *float64
}

// SalesGrowth7d estimates the sales growth over 7 days from the baseline.
// With less than a day between the baseline and the current collection there
// is no estimate (nil). With more than 7 days, the difference is scaled to 7.
func SalesGrowth7d(in TrendInput) *int64 {
	days := in.CollectedAt.Sub(in.BaseCollectedAt).Hours() / 24
	if days < 1 {
		return nil
	}
	delta := max(0, in.Sales-in.BaseSales)
	v := int64(math.Round(float64(delta) * 7 / days))
	return &v
}

// RawScore is the score before normalization: the sales growth over 7 days
// (in log scale, so a viral product does not flatten the rest), weighted by
// the commission and the rating.
//
//   - commission: from 0.5× (0%) to 1.5× (30% or more);
//   - rating: from 0.5× (0 stars) to 1× (5 stars); without reviews, 0.75×.
func RawScore(growth7d *int64, commissionBP int32, rating *float64) float64 {
	if growth7d == nil || *growth7d <= 0 {
		return 0
	}
	fc := 0.5 + float64(min(max(commissionBP, 0), 3000))/3000
	fr := 0.75
	if rating != nil {
		fr = 0.5 + 0.5*min(max(*rating, 0), 5)/5
	}
	return math.Log1p(float64(*growth7d)) * fc * fr
}

// NormalizeScores brings the raw scores to 0..100, relative to the highest,
// with one decimal place.
func NormalizeScores(raw []float64) []float64 {
	highest := 0.0
	for _, r := range raw {
		highest = max(highest, r)
	}
	out := make([]float64, len(raw))
	if highest == 0 {
		return out
	}
	for i, r := range raw {
		out[i] = math.Round(1000*r/highest) / 10
	}
	return out
}

// Trend is a product of the radar: a copy of its display data plus the score.
type Trend struct {
	ProductID            uuid.UUID
	Name                 string
	ImageURL             *string
	ShopName             string
	URL                  string
	CategoryID           *int64
	Categories           []int64
	MinPriceCents        int64
	MaxPriceCents        int64
	CommissionBP         int32
	EarningsPerSaleCents int64
	Sales                int64
	Rating               *float64
	Score                float64
	SalesGrowth7d        *int64
	UpdatedAt            time.Time
}

// TrendOfProduct is the radar view of a product that left the radar (no
// score).
func TrendOfProduct(p Product) Trend {
	return Trend{
		ProductID: p.ID, Name: p.Name, ImageURL: p.ImageURL, ShopName: p.ShopName, URL: p.URL,
		CategoryID: p.CategoryID, Categories: p.Categories, MinPriceCents: p.MinPriceCents,
		MaxPriceCents: p.MaxPriceCents, CommissionBP: p.CommissionBP,
		EarningsPerSaleCents: p.EarningsPerSaleCents(), Sales: p.Sales, Rating: p.Rating,
		UpdatedAt: p.CollectedAt,
	}
}

// RadarSort orders the radar.
type RadarSort string

const (
	SortTrend      RadarSort = "trend"
	SortCommission RadarSort = "commission"
	SortEarnings   RadarSort = "earnings"
	SortSales      RadarSort = "sales"
)

// RadarFilter filters and pages the radar. Prices in cents, commission in
// basis points.
type RadarFilter struct {
	Category      *int64
	MinPrice      *int64
	MaxPrice      *int64
	MinCommission *int32
	MinRating     *float64
	Query         string
	Sort          RadarSort
	Page          int
	PerPage       int
}

type RadarPage struct {
	Items     []Trend
	Total     int64
	Page      int
	PerPage   int
	UpdatedAt *time.Time
}

// ProductDetail is a product of the radar with its history.
type ProductDetail struct {
	Product Trend
	History []Snapshot
}

type RadarCategory struct {
	ID       int64
	Name     string
	Products int64
}

// TrendRepository keeps the radar (table trends).
type TrendRepository interface {
	// Replace saves the trends computed at computedAt and drops the products
	// computed before (they left the radar).
	Replace(ctx context.Context, computedAt time.Time, ts []Trend) error
	// Radar lists a page; the filter is already validated, and `pattern` is
	// the escaped LIKE pattern of the query.
	Radar(ctx context.Context, f RadarFilter, pattern string) ([]Trend, int64, error)
	UpdatedAt(ctx context.Context) (*time.Time, error)
	Trend(ctx context.Context, productID uuid.UUID) (Trend, error)
	// Categories counts the radar products by level 1 category.
	Categories(ctx context.Context) ([]RadarCategory, error)
}

// TrendQueue schedules the computation of the radar.
type TrendQueue interface {
	EnqueueTrends(ctx context.Context) error
}

var (
	ErrInvalidPage          = NewError(KindInvalid, "invalid_page")
	ErrInvalidPerPage       = NewError(KindInvalid, "invalid_per_page")
	ErrNegativePrice        = NewError(KindInvalid, "negative_price")
	ErrInvalidMinCommission = NewError(KindInvalid, "invalid_min_commission")
	ErrInvalidMinRating     = NewError(KindInvalid, "invalid_min_rating")
	ErrInvalidSort          = NewError(KindInvalid, "invalid_sort")
	ErrQueryTooLong         = NewError(KindInvalid, "query_too_long")
	ErrInvalidHistoryDays   = NewError(KindInvalid, "invalid_history_days")
)
