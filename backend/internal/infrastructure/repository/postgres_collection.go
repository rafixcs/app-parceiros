package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresCollection is the domain.CollectionRepository on Postgres. Every
// call runs with the scope of the actor (user and workspace), and every query
// also filters by both.
type PostgresCollection struct {
	pool *pgxpool.Pool
}

var _ domain.CollectionRepository = (*PostgresCollection)(nil)

func NewPostgresCollection(pool *pgxpool.Pool) *PostgresCollection {
	return &PostgresCollection{pool: pool}
}

func (r *PostgresCollection) run(ctx context.Context, a domain.Actor, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

func (r *PostgresCollection) CreateItem(ctx context.Context, a domain.Actor, ni domain.NewSavedItem) (domain.SavedItem, bool, error) {
	var row dbgen.SavedItem
	created := true
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		row, err = q.CreateSavedItem(ctx, dbgen.CreateSavedItemParams{
			WorkspaceID: a.WorkspaceID, UserID: a.UserID, ProductID: ni.ProductID,
			Title: ni.Title, Notes: ni.Notes, LinkStatus: dbgen.LinkStatus(ni.LinkStatus),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			created = false
			row, err = q.SavedItemByProduct(ctx, dbgen.SavedItemByProductParams{
				WorkspaceID: a.WorkspaceID, UserID: a.UserID, ProductID: ni.ProductID,
			})
		}
		return err
	})
	if err != nil {
		return domain.SavedItem{}, false, err
	}
	return savedItemOf(row), created, nil
}

func (r *PostgresCollection) Item(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.SavedItem, error) {
	var row dbgen.SavedItem
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		row, err = q.SavedItemByID(ctx, dbgen.SavedItemByIDParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		return err
	})
	return savedItemOf(row), notFound(err)
}

func (r *PostgresCollection) ItemsByProducts(ctx context.Context, a domain.Actor, productIDs []uuid.UUID) ([]domain.SavedItem, error) {
	var out []domain.SavedItem
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.SavedItemsByProducts(ctx, dbgen.SavedItemsByProductsParams{
			WorkspaceID: a.WorkspaceID, UserID: a.UserID, ProductIds: productIDs,
		})
		out = collectionRows(rows, savedItemOf)
		return err
	})
	return out, err
}

func (r *PostgresCollection) ListItems(ctx context.Context, a domain.Actor, iq domain.ItemQuery) ([]domain.SavedItem, int64, error) {
	p := dbgen.ListSavedItemsParams{
		WorkspaceID: a.WorkspaceID, UserID: a.UserID, CollectionID: iq.CollectionID,
		RowLimit: int32(iq.Limit), RowOffset: int32(iq.Offset),
	}
	if iq.Query != "" {
		p.Query = &iq.Query
	}
	if iq.Tag != "" {
		p.Tag = &iq.Tag
	}
	if iq.Status != "" {
		st := dbgen.ItemStatus(iq.Status)
		p.Status = &st
	}
	var out []domain.SavedItem
	var total int64
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ListSavedItems(ctx, p)
		if err != nil {
			return err
		}
		out = make([]domain.SavedItem, len(rows))
		for i, row := range rows {
			total = row.Total
			out[i] = savedItemOf(dbgen.SavedItem{
				ID: row.ID, WorkspaceID: row.WorkspaceID, UserID: row.UserID, ProductID: row.ProductID,
				Title: row.Title, Description: row.Description, Notes: row.Notes, Tags: row.Tags,
				Status: row.Status, AffiliateLink: row.AffiliateLink, LinkOrigin: row.LinkOrigin,
				LinkStatus: row.LinkStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
		return nil
	})
	return out, total, err
}

func (r *PostgresCollection) UpdateItem(ctx context.Context, a domain.Actor, id uuid.UUID, f domain.ItemFields) (domain.SavedItem, error) {
	p := dbgen.UpdateSavedItemParams{
		ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID,
		Title: f.Title, Description: f.Description, Notes: f.Notes, Tags: f.Tags,
	}
	if f.Status != nil {
		st := dbgen.ItemStatus(*f.Status)
		p.Status = &st
	}
	var row dbgen.SavedItem
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		row, err = q.UpdateSavedItem(ctx, p)
		return err
	})
	return savedItemOf(row), notFound(err)
}

func (r *PostgresCollection) SetManualLink(ctx context.Context, a domain.Actor, id uuid.UUID, link string) (domain.SavedItem, error) {
	var row dbgen.SavedItem
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		row, err = q.SetManualLink(ctx, dbgen.SetManualLinkParams{
			ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, AffiliateLink: &link,
		})
		return err
	})
	return savedItemOf(row), notFound(err)
}

func (r *PostgresCollection) ResetAutoLink(ctx context.Context, a domain.Actor, id uuid.UUID, status domain.LinkStatus) (domain.SavedItem, error) {
	var row dbgen.SavedItem
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		if err := q.DeleteChannelLinks(ctx, dbgen.DeleteChannelLinksParams{ItemID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID}); err != nil {
			return err
		}
		var err error
		row, err = q.ResetAutoLink(ctx, dbgen.ResetAutoLinkParams{
			ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, LinkStatus: dbgen.LinkStatus(status),
		})
		return err
	})
	return savedItemOf(row), notFound(err)
}

func (r *PostgresCollection) MarkLinkStatus(ctx context.Context, a domain.Actor, id uuid.UUID, status domain.LinkStatus) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.MarkLinkStatus(ctx, dbgen.MarkLinkStatusParams{
			ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, LinkStatus: dbgen.LinkStatus(status),
		})
	})
}

func (r *PostgresCollection) CompleteAutoLink(ctx context.Context, a domain.Actor, id uuid.UUID, links []domain.ChannelLink, main string) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		n, err := q.CompleteAutoLink(ctx, dbgen.CompleteAutoLinkParams{
			ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, AffiliateLink: &main,
		})
		if err != nil || n == 0 {
			// Removed, or switched to a manual link while the job ran.
			return err
		}
		for _, l := range links {
			if err := q.SaveChannelLink(ctx, dbgen.SaveChannelLinkParams{
				ItemID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID,
				Channel: dbgen.Channel(l.Channel), SubID: l.SubID, URL: l.URL,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresCollection) ClaimPendingLinks(ctx context.Context, a domain.Actor) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		ids, err = q.ClaimPendingLinks(ctx, dbgen.ClaimPendingLinksParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		return err
	})
	return ids, err
}

func (r *PostgresCollection) DeleteItem(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		n, err := q.DeleteSavedItem(ctx, dbgen.DeleteSavedItemParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresCollection) SavedProductIDs(ctx context.Context, a domain.Actor) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		ids, err = q.SavedProductIDs(ctx, dbgen.SavedProductIDsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		return err
	})
	return ids, err
}

func (r *PostgresCollection) ItemLinks(ctx context.Context, a domain.Actor, itemIDs []uuid.UUID) (map[uuid.UUID][]domain.ChannelLink, error) {
	out := map[uuid.UUID][]domain.ChannelLink{}
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ChannelLinksOfItems(ctx, dbgen.ChannelLinksOfItemsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Ids: itemIDs})
		for _, l := range rows {
			out[l.ItemID] = append(out[l.ItemID], domain.ChannelLink{Channel: domain.Channel(l.Channel), SubID: l.SubID, URL: l.URL})
		}
		return err
	})
	return out, err
}

func (r *PostgresCollection) ItemCollections(ctx context.Context, a domain.Actor, itemIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.CollectionsOfItems(ctx, dbgen.CollectionsOfItemsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Ids: itemIDs})
		for _, c := range rows {
			out[c.ItemID] = append(out[c.ItemID], c.CollectionID)
		}
		return err
	})
	return out, err
}

func (r *PostgresCollection) Collections(ctx context.Context, a domain.Actor) ([]domain.Collection, error) {
	var out []domain.Collection
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ListCollections(ctx, dbgen.ListCollectionsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		out = collectionRows(rows, func(c dbgen.ListCollectionsRow) domain.Collection {
			return domain.Collection{ID: c.ID, Name: c.Name, Items: c.Items, CreatedAt: c.CreatedAt}
		})
		return err
	})
	return out, err
}

func (r *PostgresCollection) Collection(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Collection, error) {
	var c dbgen.CollectionByIDRow
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		c, err = q.CollectionByID(ctx, dbgen.CollectionByIDParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		return err
	})
	return domain.Collection{ID: c.ID, Name: c.Name, Items: c.Items, CreatedAt: c.CreatedAt}, notFound(err)
}

func (r *PostgresCollection) CollectionByName(ctx context.Context, a domain.Actor, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		id, err = q.CollectionByName(ctx, dbgen.CollectionByNameParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Name: name})
		return err
	})
	return id, notFound(err)
}

func (r *PostgresCollection) CreateCollection(ctx context.Context, a domain.Actor, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		id, err = q.CreateCollection(ctx, dbgen.CreateCollectionParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Name: name})
		return err
	})
	if isUniqueViolation(err) {
		return uuid.Nil, domain.ErrCollectionExists
	}
	return id, err
}

func (r *PostgresCollection) RenameCollection(ctx context.Context, a domain.Actor, id uuid.UUID, name string) error {
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		n, err := q.RenameCollection(ctx, dbgen.RenameCollectionParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, Name: name})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
	if isUniqueViolation(err) {
		return domain.ErrCollectionExists
	}
	return err
}

func (r *PostgresCollection) DeleteCollection(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		n, err := q.DeleteCollection(ctx, dbgen.DeleteCollectionParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresCollection) CountCollections(ctx context.Context, a domain.Actor, ids []uuid.UUID) (int64, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		n, err = q.CountCollections(ctx, dbgen.CountCollectionsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Ids: ids})
		return err
	})
	return n, err
}

func (r *PostgresCollection) SetItemCollections(ctx context.Context, a domain.Actor, itemID uuid.UUID, collectionIDs []uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		if err := q.ClearItemCollections(ctx, dbgen.ClearItemCollectionsParams{ItemID: itemID, WorkspaceID: a.WorkspaceID, UserID: a.UserID}); err != nil {
			return err
		}
		for _, c := range collectionIDs {
			if err := q.AddCollectionItem(ctx, dbgen.AddCollectionItemParams{
				CollectionID: c, ItemID: itemID, WorkspaceID: a.WorkspaceID, UserID: a.UserID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresCollection) AddToCollection(ctx context.Context, a domain.Actor, collectionID, itemID uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.AddCollectionItem(ctx, dbgen.AddCollectionItemParams{
			CollectionID: collectionID, ItemID: itemID, WorkspaceID: a.WorkspaceID, UserID: a.UserID,
		})
	})
}

func savedItemOf(r dbgen.SavedItem) domain.SavedItem {
	return domain.SavedItem{
		ID: r.ID, ProductID: r.ProductID, Title: r.Title, Description: r.Description, Notes: r.Notes,
		Tags: r.Tags, Status: domain.ItemStatus(r.Status), AffiliateLink: r.AffiliateLink,
		LinkOrigin: domain.LinkOrigin(r.LinkOrigin), LinkStatus: domain.LinkStatus(r.LinkStatus),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// collectionRows converts a slice of rows.
func collectionRows[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}
