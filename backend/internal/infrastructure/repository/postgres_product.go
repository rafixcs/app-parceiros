package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresProduct keeps the global catalog. It is not customer data: reads go
// through the API role (with an empty scope) and writes use the owner role of
// the tables, because they create the monthly partitions of the snapshots.
type PostgresProduct struct {
	pool *pgxpool.Pool
}

func NewPostgresProduct(pool *pgxpool.Pool) *PostgresProduct { return &PostgresProduct{pool: pool} }

var _ domain.ProductRepository = (*PostgresProduct)(nil)

// owner runs fn in a transaction with the owner role of the tables.
func (r *PostgresProduct) owner(ctx context.Context, fn func(*dbgen.Queries) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(dbgen.New(tx)) })
}

func (r *PostgresProduct) read(ctx context.Context, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, database.Scope{}, func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

// Record saves the offers of one collection. Collections in the same hour
// count as one; a product already updated by a newer collection stays as is.
func (r *PostgresProduct) Record(ctx context.Context, source domain.Source, collectedAt time.Time, offers []domain.Offer) error {
	collectedAt = collectedAt.UTC().Truncate(time.Hour)
	if len(offers) == 0 {
		return nil
	}
	return r.owner(ctx, func(q *dbgen.Queries) error {
		if err := q.EnsureSnapshotPartition(ctx, collectedAt); err != nil {
			return fmt.Errorf("creating the snapshot partition: %w", err)
		}
		params := make([]dbgen.UpsertProductParams, len(offers))
		for i, o := range offers {
			var cat *int64
			if len(o.Categories) > 0 {
				cat = &o.Categories[0]
			}
			var img *string
			if o.ImageURL != "" {
				img = &o.ImageURL
			}
			categories := o.Categories
			if categories == nil {
				categories = []int64{}
			}
			params[i] = dbgen.UpsertProductParams{
				Source: dbgen.Source(source), ItemID: o.ItemID, ShopID: o.ShopID, ShopName: o.ShopName,
				Name: o.Name, ImageURL: img, CategoryID: cat, Categories: categories, URL: o.URL,
				MinPriceCents: o.MinPriceCents, MaxPriceCents: o.MaxPriceCents,
				CommissionBp: o.CommissionBP, Sales: o.Sales, Rating: o.Rating, CollectedAt: collectedAt,
			}
		}

		ids := make([]uuid.UUID, len(offers))
		var batchErr error
		q.UpsertProduct(ctx, params).QueryRow(func(i int, id uuid.UUID, err error) {
			switch {
			case err == nil:
				ids[i] = id
			case errors.Is(err, pgx.ErrNoRows):
				// A newer collection already updated the product.
			case batchErr == nil:
				batchErr = err
			}
		})
		if batchErr != nil {
			return batchErr
		}
		for i, id := range ids {
			if id != uuid.Nil {
				continue
			}
			if ids[i], batchErr = q.ProductIDByItem(ctx, dbgen.ProductIDByItemParams{
				Source: dbgen.Source(source), ItemID: offers[i].ItemID,
			}); batchErr != nil {
				return batchErr
			}
		}

		snaps := make([]dbgen.InsertSnapshotParams, len(offers))
		for i, o := range offers {
			snaps[i] = dbgen.InsertSnapshotParams{
				ProductID: ids[i], CollectedAt: collectedAt,
				MinPriceCents: o.MinPriceCents, MaxPriceCents: o.MaxPriceCents,
				CommissionBp: o.CommissionBP, Sales: o.Sales, Rating: o.Rating,
			}
		}
		q.InsertSnapshot(ctx, snaps).Exec(func(_ int, err error) {
			if err != nil && batchErr == nil {
				batchErr = err
			}
		})
		return batchErr
	})
}

func (r *PostgresProduct) MonitoredCategories(ctx context.Context, source domain.Source) ([]int64, error) {
	return dbgen.New(r.pool).MonitoredCategories(ctx, dbgen.Source(source))
}

func (r *PostgresProduct) SaveCategories(ctx context.Context, source domain.Source, cs []domain.Category) error {
	return r.owner(ctx, func(q *dbgen.Queries) error {
		for _, c := range cs {
			if err := q.UpsertCategory(ctx, dbgen.UpsertCategoryParams{
				Source: dbgen.Source(source), ID: c.ID, Name: c.Name, Monitored: c.Monitored,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresProduct) CategoryNames(ctx context.Context, source domain.Source, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	err := r.read(ctx, func(q *dbgen.Queries) error {
		rows, err := q.CategoryNames(ctx, dbgen.CategoryNamesParams{Source: dbgen.Source(source), Ids: ids})
		for _, row := range rows {
			out[row.ID] = row.Name
		}
		return err
	})
	return out, err
}

func (r *PostgresProduct) ForTrends(ctx context.Context, since time.Time) ([]domain.ProductBaseline, error) {
	rows, err := dbgen.New(r.pool).ProductsForTrends(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ProductBaseline, len(rows))
	for i, row := range rows {
		out[i] = domain.ProductBaseline{
			Product: domain.Product{
				ID: row.ID, Source: domain.Source(row.Source), ItemID: row.ItemID, ShopName: row.ShopName,
				Name: row.Name, ImageURL: row.ImageURL, CategoryID: row.CategoryID, Categories: row.Categories,
				URL: row.URL, MinPriceCents: row.MinPriceCents, MaxPriceCents: row.MaxPriceCents,
				CommissionBP: row.CommissionBp, Sales: row.Sales, Rating: row.Rating, CollectedAt: row.CollectedAt,
			},
			BaseSales:       row.BaseSales,
			BaseCollectedAt: row.BaseCollectedAt,
		}
	}
	return out, nil
}

func (r *PostgresProduct) Product(ctx context.Context, id uuid.UUID) (domain.Product, error) {
	var p dbgen.Product
	err := r.read(ctx, func(q *dbgen.Queries) (err error) {
		p, err = q.ProductByID(ctx, id)
		return err
	})
	if err != nil {
		return domain.Product{}, notFound(err)
	}
	return productOf(p), nil
}

func (r *PostgresProduct) Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error) {
	out := make(map[uuid.UUID]domain.Product, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	err := r.read(ctx, func(q *dbgen.Queries) error {
		rows, err := q.ProductsByIDs(ctx, ids)
		for _, p := range rows {
			out[p.ID] = productOf(p)
		}
		return err
	})
	return out, err
}

func (r *PostgresProduct) ProductByItem(ctx context.Context, source domain.Source, itemID int64) (domain.Product, error) {
	var p dbgen.Product
	err := r.read(ctx, func(q *dbgen.Queries) (err error) {
		p, err = q.ProductByItem(ctx, dbgen.ProductByItemParams{Source: dbgen.Source(source), ItemID: itemID})
		return err
	})
	if err != nil {
		return domain.Product{}, notFound(err)
	}
	return productOf(p), nil
}

func (r *PostgresProduct) History(ctx context.Context, id uuid.UUID, since time.Time) ([]domain.Snapshot, error) {
	var out []domain.Snapshot
	err := r.read(ctx, func(q *dbgen.Queries) error {
		rows, err := q.ProductHistory(ctx, dbgen.ProductHistoryParams{ProductID: id, Since: since})
		if err != nil {
			return err
		}
		out = make([]domain.Snapshot, len(rows))
		for i, row := range rows {
			out[i] = domain.Snapshot{
				CollectedAt: row.CollectedAt, MinPriceCents: row.MinPriceCents, MaxPriceCents: row.MaxPriceCents,
				CommissionBP: row.CommissionBp, Sales: row.Sales, Rating: row.Rating,
			}
		}
		return nil
	})
	return out, err
}

func productOf(p dbgen.Product) domain.Product {
	return domain.Product{
		ID: p.ID, Source: domain.Source(p.Source), ItemID: p.ItemID, ShopName: p.ShopName, Name: p.Name,
		ImageURL: p.ImageURL, CategoryID: p.CategoryID, Categories: p.Categories, URL: p.URL,
		MinPriceCents: p.MinPriceCents, MaxPriceCents: p.MaxPriceCents,
		CommissionBP: p.CommissionBp, Sales: p.Sales, Rating: p.Rating, CollectedAt: p.CollectedAt,
	}
}
