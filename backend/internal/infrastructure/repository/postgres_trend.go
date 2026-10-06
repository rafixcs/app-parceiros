package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresTrend keeps the radar (table trends), global like the catalog: the
// worker writes it with the owner role and the API reads it.
type PostgresTrend struct {
	pool *pgxpool.Pool
}

func NewPostgresTrend(pool *pgxpool.Pool) *PostgresTrend { return &PostgresTrend{pool: pool} }

var _ domain.TrendRepository = (*PostgresTrend)(nil)

func (r *PostgresTrend) read(ctx context.Context, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, database.Scope{}, func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

func (r *PostgresTrend) Replace(ctx context.Context, computedAt time.Time, ts []domain.Trend) error {
	params := make([]dbgen.UpsertTrendParams, len(ts))
	for i, t := range ts {
		params[i] = dbgen.UpsertTrendParams{
			ProductID: t.ProductID, ComputedAt: computedAt, Score: t.Score,
			EarningsPerSaleCents: t.EarningsPerSaleCents, SalesGrowth7d: t.SalesGrowth7d,
			Name: t.Name, ShopName: t.ShopName, ImageURL: t.ImageURL, CategoryID: t.CategoryID,
			Categories: t.Categories, URL: t.URL, MinPriceCents: t.MinPriceCents,
			MaxPriceCents: t.MaxPriceCents, CommissionBp: t.CommissionBP, Sales: t.Sales,
			Rating: t.Rating, UpdatedAt: t.UpdatedAt,
		}
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		var batchErr error
		q.UpsertTrend(ctx, params).Exec(func(_ int, err error) {
			if err != nil && batchErr == nil {
				batchErr = err
			}
		})
		if batchErr != nil {
			return batchErr
		}
		_, err := q.DeleteStaleTrends(ctx, computedAt)
		return err
	})
}

func (r *PostgresTrend) Radar(ctx context.Context, f domain.RadarFilter, pattern string) ([]domain.Trend, int64, error) {
	p := dbgen.RadarParams{
		Category: f.Category, MinPrice: f.MinPrice, MaxPrice: f.MaxPrice, MinCommission: f.MinCommission,
		MinRating: f.MinRating, Sort: string(f.Sort),
		RowLimit: int32(f.PerPage), RowOffset: int32((f.Page - 1) * f.PerPage),
	}
	if f.Query != "" {
		p.Query, p.Pattern = &f.Query, &pattern
	}
	var (
		out   []domain.Trend
		total int64
	)
	err := r.read(ctx, func(q *dbgen.Queries) error {
		rows, err := q.Radar(ctx, p)
		for _, row := range rows {
			total = row.Total
			out = append(out, domain.Trend{
				ProductID: row.ProductID, Name: row.Name, ImageURL: row.ImageURL, ShopName: row.ShopName,
				URL: row.URL, Categories: row.Categories, MinPriceCents: row.MinPriceCents,
				MaxPriceCents: row.MaxPriceCents, CommissionBP: row.CommissionBp,
				EarningsPerSaleCents: row.EarningsPerSaleCents, Sales: row.Sales, Rating: row.Rating,
				Score: row.Score, SalesGrowth7d: row.SalesGrowth7d, UpdatedAt: row.UpdatedAt,
			})
		}
		return err
	})
	return out, total, err
}

func (r *PostgresTrend) UpdatedAt(ctx context.Context) (*time.Time, error) {
	var at time.Time
	err := r.read(ctx, func(q *dbgen.Queries) (err error) {
		at, err = q.RadarUpdatedAt(ctx)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &at, nil
}

func (r *PostgresTrend) Trend(ctx context.Context, productID uuid.UUID) (domain.Trend, error) {
	var row dbgen.RadarItemRow
	err := r.read(ctx, func(q *dbgen.Queries) (err error) {
		row, err = q.RadarItem(ctx, productID)
		return err
	})
	if err != nil {
		return domain.Trend{}, notFound(err)
	}
	return domain.Trend{
		ProductID: row.ProductID, Name: row.Name, ImageURL: row.ImageURL, ShopName: row.ShopName,
		URL: row.URL, Categories: row.Categories, MinPriceCents: row.MinPriceCents,
		MaxPriceCents: row.MaxPriceCents, CommissionBP: row.CommissionBp,
		EarningsPerSaleCents: row.EarningsPerSaleCents, Sales: row.Sales, Rating: row.Rating,
		Score: row.Score, SalesGrowth7d: row.SalesGrowth7d, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *PostgresTrend) Categories(ctx context.Context) ([]domain.RadarCategory, error) {
	var out []domain.RadarCategory
	err := r.read(ctx, func(q *dbgen.Queries) error {
		rows, err := q.RadarCategories(ctx)
		for _, row := range rows {
			out = append(out, domain.RadarCategory{ID: row.ID, Products: row.Products})
		}
		return err
	})
	return out, err
}
