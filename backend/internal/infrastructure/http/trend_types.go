package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type trendResponse struct {
	ProductID            uuid.UUID `json:"product_id"`
	Name                 string    `json:"name"`
	ImageURL             *string   `json:"image_url"`
	ShopName             string    `json:"shop_name"`
	URL                  string    `json:"url"`
	Categories           []int64   `json:"categories"`
	MinPriceCents        int64     `json:"min_price_cents"`
	MaxPriceCents        int64     `json:"max_price_cents"`
	CommissionBP         int32     `json:"commission_bp"`
	EarningsPerSaleCents int64     `json:"earnings_per_sale_cents"`
	Sales                int64     `json:"sales"`
	Rating               *float64  `json:"rating"`
	// Score goes from 0 to 100 (0 for a product that left the radar).
	Score float64 `json:"score"`
	// SalesGrowth7d is the estimated sales growth over 7 days; null without a
	// day of history.
	SalesGrowth7d *int64    `json:"sales_growth_7d"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func trendResponseOf(t domain.Trend) trendResponse {
	categories := t.Categories
	if categories == nil {
		categories = []int64{}
	}
	return trendResponse{
		ProductID: t.ProductID, Name: t.Name, ImageURL: t.ImageURL, ShopName: t.ShopName, URL: t.URL,
		Categories: categories, MinPriceCents: t.MinPriceCents, MaxPriceCents: t.MaxPriceCents,
		CommissionBP: t.CommissionBP, EarningsPerSaleCents: t.EarningsPerSaleCents, Sales: t.Sales,
		Rating: t.Rating, Score: t.Score, SalesGrowth7d: t.SalesGrowth7d, UpdatedAt: t.UpdatedAt,
	}
}

type radarPageResponse struct {
	Items     []trendResponse `json:"items"`
	Total     int64           `json:"total"`
	Page      int             `json:"page"`
	PerPage   int             `json:"per_page"`
	UpdatedAt *time.Time      `json:"updated_at"`
}

type radarCategoryResponse struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Products int64  `json:"products"`
}

type snapshotResponse struct {
	CollectedAt   time.Time `json:"collected_at"`
	MinPriceCents int64     `json:"min_price_cents"`
	MaxPriceCents int64     `json:"max_price_cents"`
	CommissionBP  int32     `json:"commission_bp"`
	Sales         int64     `json:"sales"`
	Rating        *float64  `json:"rating"`
}

func snapshotResponseOf(s domain.Snapshot) snapshotResponse {
	return snapshotResponse{
		CollectedAt: s.CollectedAt, MinPriceCents: s.MinPriceCents, MaxPriceCents: s.MaxPriceCents,
		CommissionBP: s.CommissionBP, Sales: s.Sales, Rating: s.Rating,
	}
}

type productDetailResponse struct {
	Product trendResponse      `json:"product"`
	History []snapshotResponse `json:"history"`
}
