package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresResult is the domain.ResultRepository on Postgres. The reads run
// with the scope of the actor; the writes of conversions run as the user
// without a workspace, the only scope the RLS lets write.
type PostgresResult struct {
	pool *pgxpool.Pool
}

var _ domain.ResultRepository = (*PostgresResult)(nil)

func NewPostgresResult(pool *pgxpool.Pool) *PostgresResult {
	return &PostgresResult{pool: pool}
}

func (r *PostgresResult) run(ctx context.Context, a domain.Actor, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

func (r *PostgresResult) SaveConversions(ctx context.Context, userID uuid.UUID, cs []domain.StoredConversion) error {
	return r.run(ctx, domain.Actor{UserID: userID}, func(q *dbgen.Queries) error {
		for _, c := range cs {
			var ch *dbgen.Channel
			if c.Channel != nil {
				v := dbgen.Channel(*c.Channel)
				ch = &v
			}
			err := q.SaveConversion(ctx, dbgen.SaveConversionParams{
				UserID: userID, WorkspaceID: c.WorkspaceID, Source: string(domain.SourceShopee),
				ConversionID: c.ConversionID, OrderID: c.OrderID, ItemID: c.ItemID, ModelID: c.ModelID,
				ProductID: c.ProductID, ItemName: c.ItemName, ShopName: c.ShopName, SubID: c.SubID,
				Channel: ch, Status: dbgen.OrderStatus(c.Status), Quantity: c.Quantity,
				AmountCents: c.PriceCents * int64(c.Quantity), CommissionCents: c.CommissionCents,
				OccurredAt: c.PurchasedAt, ClickedAt: c.ClickedAt,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresResult) Totals(ctx context.Context, a domain.Actor, rq domain.ResultQuery) (domain.ResultTotals, int64, error) {
	var t dbgen.ResultTotalsRow
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		t, err = q.ResultTotals(ctx, dbgen.ResultTotalsParams{
			WorkspaceID: rq.WorkspaceID, UserIds: nonNilIDs(rq.UserIDs), FromTime: rq.Start, ToTime: rq.End,
		})
		return err
	})
	return domain.ResultTotals{
		Orders: t.Orders, Cancelled: t.Cancelled, Items: t.Items, SalesCents: t.SalesCents,
		EstimatedCommissionCents: t.EstimatedCommissionCents, ValidatedCommissionCents: t.ValidatedCommissionCents,
	}, t.ActiveUsers, err
}

func (r *PostgresResult) ByDay(ctx context.Context, a domain.Actor, rq domain.ResultQuery) ([]domain.DayResult, error) {
	var out []domain.DayResult
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ResultsByDay(ctx, dbgen.ResultsByDayParams{
			WorkspaceID: rq.WorkspaceID, UserIds: nonNilIDs(rq.UserIDs), FromTime: rq.Start, ToTime: rq.End,
		})
		out = resultRows(rows, func(d dbgen.ResultsByDayRow) domain.DayResult {
			return domain.DayResult{
				Day: d.Day, Orders: d.Orders,
				EstimatedCommissionCents: d.EstimatedCommissionCents, ValidatedCommissionCents: d.ValidatedCommissionCents,
			}
		})
		return err
	})
	return out, err
}

func (r *PostgresResult) ByProduct(ctx context.Context, a domain.Actor, rq domain.ResultQuery, limit int) ([]domain.ProductResult, error) {
	var out []domain.ProductResult
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ResultsByProduct(ctx, dbgen.ResultsByProductParams{
			WorkspaceID: rq.WorkspaceID, UserIds: nonNilIDs(rq.UserIDs), FromTime: rq.Start, ToTime: rq.End,
			RowLimit: int32(limit),
		})
		out = resultRows(rows, func(p dbgen.ResultsByProductRow) domain.ProductResult {
			pr := domain.ProductResult{
				ItemID: p.ItemID, Name: p.ItemName, ShopName: p.ShopName, Orders: p.Orders, Items: p.Items,
				SalesCents: p.SalesCents, EstimatedCommissionCents: p.EstimatedCommissionCents,
				ValidatedCommissionCents: p.ValidatedCommissionCents,
			}
			if id, err := uuid.Parse(p.ProductID); err == nil {
				pr.ProductID = &id
			}
			return pr
		})
		return err
	})
	return out, err
}

func (r *PostgresResult) ByChannel(ctx context.Context, a domain.Actor, rq domain.ResultQuery) ([]domain.ChannelResult, error) {
	var out []domain.ChannelResult
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ResultsByChannel(ctx, dbgen.ResultsByChannelParams{
			WorkspaceID: rq.WorkspaceID, UserIds: nonNilIDs(rq.UserIDs), FromTime: rq.Start, ToTime: rq.End,
		})
		out = resultRows(rows, func(c dbgen.ResultsByChannelRow) domain.ChannelResult {
			return domain.ChannelResult{
				Channel: domain.Channel(c.Channel), Orders: c.Orders,
				EstimatedCommissionCents: c.EstimatedCommissionCents, ValidatedCommissionCents: c.ValidatedCommissionCents,
			}
		})
		return err
	})
	return out, err
}

func (r *PostgresResult) ByImportGroup(ctx context.Context, a domain.Actor, rq domain.ResultQuery, imports []domain.ResultImport) (map[int]domain.ListResult, error) {
	out := map[int]domain.ListResult{}
	if len(imports) == 0 {
		return out, nil
	}
	p := dbgen.ResultsByImportGroupParams{WorkspaceID: rq.WorkspaceID, FromTime: rq.Start, ToTime: rq.End}
	for _, i := range imports {
		p.GroupIds = append(p.GroupIds, int32(i.Group))
		p.ImportUsers = append(p.ImportUsers, i.UserID)
		p.ImportProducts = append(p.ImportProducts, i.ProductID)
		p.ImportedAt = append(p.ImportedAt, i.Since)
	}
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ResultsByImportGroup(ctx, p)
		for _, g := range rows {
			out[int(g.GroupID)] = domain.ListResult{
				Orders: g.Orders, SalesCents: g.SalesCents,
				EstimatedCommissionCents: g.EstimatedCommissionCents, ValidatedCommissionCents: g.ValidatedCommissionCents,
			}
		}
		return err
	})
	return out, err
}

func (r *PostgresResult) ConversionSync(ctx context.Context, userID uuid.UUID) (domain.ConversionSync, error) {
	var row dbgen.ConversionSync
	err := r.run(ctx, domain.Actor{UserID: userID}, func(q *dbgen.Queries) (err error) {
		row, err = q.ConversionSyncByUser(ctx, userID)
		return err
	})
	if err != nil {
		return domain.ConversionSync{}, notFound(err)
	}
	return conversionSyncOf(row), nil
}

func (r *PostgresResult) StartConversionSync(ctx context.Context, userID uuid.UUID, now time.Time) (domain.ConversionSync, error) {
	var row dbgen.ConversionSync
	err := r.run(ctx, domain.Actor{UserID: userID}, func(q *dbgen.Queries) (err error) {
		row, err = q.StartConversionSync(ctx, dbgen.StartConversionSyncParams{UserID: userID, Now: now})
		return err
	})
	return conversionSyncOf(row), err
}

func (r *PostgresResult) FinishConversionSync(ctx context.Context, userID uuid.UUID, status domain.SyncStatus,
	now time.Time, conversions int, errorCode *string,
) error {
	return r.run(ctx, domain.Actor{UserID: userID}, func(q *dbgen.Queries) error {
		return q.FinishConversionSync(ctx, dbgen.FinishConversionSyncParams{
			UserID: userID, Status: string(status), Now: now,
			Conversions: int32(min(conversions, 1<<31-1)), Error: errorCode,
		})
	})
}

func conversionSyncOf(r dbgen.ConversionSync) domain.ConversionSync {
	requested := r.RequestedAt
	return domain.ConversionSync{
		Status: domain.SyncStatus(r.Status), RequestedAt: &requested, FinishedAt: r.FinishedAt,
		Conversions: r.Conversions, ErrorCode: r.Error,
	}
}

func resultRows[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// nonNilIDs keeps an empty list as an empty array (not NULL) in the query.
func nonNilIDs(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}
