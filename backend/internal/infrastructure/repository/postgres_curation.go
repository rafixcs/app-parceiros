package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresCuration is the domain.CurationRepository on Postgres. Every call
// runs with the scope of the member (user and workspace), and every query
// also filters by the workspace; the RLS hides the drafts from affiliates and
// lets only owner and mentors write the lists.
type PostgresCuration struct {
	pool *pgxpool.Pool
}

var _ domain.CurationRepository = (*PostgresCuration)(nil)

func NewPostgresCuration(pool *pgxpool.Pool) *PostgresCuration { return &PostgresCuration{pool: pool} }

func (r *PostgresCuration) run(ctx context.Context, a domain.Actor, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

func (r *PostgresCuration) CountLists(ctx context.Context, a domain.Actor) (int64, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		n, err = q.CountCuratedLists(ctx, a.WorkspaceID)
		return err
	})
	return n, err
}

func (r *PostgresCuration) CreateList(ctx context.Context, a domain.Actor, title, description string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		id, err = q.CreateCuratedList(ctx, dbgen.CreateCuratedListParams{
			WorkspaceID: a.WorkspaceID, AuthorID: a.UserID, Title: title, Description: description,
		})
		return err
	})
	return id, err
}

func curatedListOf(r dbgen.CuratedListRow) domain.CuratedList {
	importers := r.Importers
	return domain.CuratedList{
		ID: r.ID, Title: r.Title, Description: r.Description, PublishedAt: r.PublishedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Products: r.Products, Importers: &importers,
		ImportedByMe: r.ImportedByMe,
	}
}

func (r *PostgresCuration) Lists(ctx context.Context, a domain.Actor, drafts bool) ([]domain.CuratedList, error) {
	var out []domain.CuratedList
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.CuratedLists(ctx, dbgen.CuratedListsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, Drafts: drafts})
		out = make([]domain.CuratedList, len(rows))
		for i, row := range rows {
			out[i] = curatedListOf(dbgen.CuratedListRow(row))
		}
		return err
	})
	return out, err
}

func (r *PostgresCuration) List(ctx context.Context, a domain.Actor, id uuid.UUID, drafts bool) (domain.CuratedList, error) {
	var row dbgen.CuratedListRow
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		row, err = q.CuratedList(ctx, dbgen.CuratedListParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID, ID: id, Drafts: drafts})
		return err
	})
	if err != nil {
		return domain.CuratedList{}, notFound(err)
	}
	return curatedListOf(row), nil
}

func (r *PostgresCuration) LockList(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return notFound(r.run(ctx, a, func(q *dbgen.Queries) error {
		_, err := q.LockCuratedList(ctx, dbgen.LockCuratedListParams{ID: id, WorkspaceID: a.WorkspaceID})
		return err
	}))
}

// curationAffected turns zero rows into ErrNotFound.
func curationAffected(n int64, err error) error {
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (r *PostgresCuration) UpdateList(ctx context.Context, a domain.Actor, id uuid.UUID, title, description *string) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return curationAffected(q.UpdateCuratedList(ctx, dbgen.UpdateCuratedListParams{
			Title: title, Description: description, ID: id, WorkspaceID: a.WorkspaceID,
		}))
	})
}

func (r *PostgresCuration) PublishList(ctx context.Context, a domain.Actor, id uuid.UUID) (bool, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		n, err = q.PublishCuratedList(ctx, dbgen.PublishCuratedListParams{ID: id, WorkspaceID: a.WorkspaceID})
		return err
	})
	return n == 1, err
}

func (r *PostgresCuration) DeleteList(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return curationAffected(q.DeleteCuratedList(ctx, dbgen.DeleteCuratedListParams{ID: id, WorkspaceID: a.WorkspaceID}))
	})
}

func (r *PostgresCuration) TouchList(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.TouchCuratedList(ctx, dbgen.TouchCuratedListParams{ID: id, WorkspaceID: a.WorkspaceID})
	})
}

func (r *PostgresCuration) ListItems(ctx context.Context, a domain.Actor, id uuid.UUID) ([]domain.CuratedListItem, error) {
	var out []domain.CuratedListItem
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.CuratedListItems(ctx, dbgen.CuratedListItemsParams{ListID: id, WorkspaceID: a.WorkspaceID})
		out = make([]domain.CuratedListItem, len(rows))
		for i, row := range rows {
			out[i] = domain.CuratedListItem{ProductID: row.ProductID, Comment: row.Comment, Position: row.Position}
		}
		return err
	})
	return out, err
}

func (r *PostgresCuration) AddListItem(ctx context.Context, a domain.Actor, id, productID uuid.UUID, comment string) (bool, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) (err error) {
		n, err = q.AddCuratedListItem(ctx, dbgen.AddCuratedListItemParams{
			ListID: id, WorkspaceID: a.WorkspaceID, ProductID: productID, Comment: comment,
		})
		return err
	})
	return n == 1, err
}

func (r *PostgresCuration) CommentListItem(ctx context.Context, a domain.Actor, id, productID uuid.UUID, comment string) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return curationAffected(q.CommentCuratedListItem(ctx, dbgen.CommentCuratedListItemParams{
			Comment: comment, ListID: id, WorkspaceID: a.WorkspaceID, ProductID: productID,
		}))
	})
}

func (r *PostgresCuration) RemoveListItem(ctx context.Context, a domain.Actor, id, productID uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return curationAffected(q.RemoveCuratedListItem(ctx, dbgen.RemoveCuratedListItemParams{
			ListID: id, WorkspaceID: a.WorkspaceID, ProductID: productID,
		}))
	})
}

func (r *PostgresCuration) ReorderListItems(ctx context.Context, a domain.Actor, id uuid.UUID, productIDs []uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		_, err := q.ReorderCuratedListItems(ctx, dbgen.ReorderCuratedListItemsParams{
			ListID: id, WorkspaceID: a.WorkspaceID, ProductIds: productIDs,
		})
		return err
	})
}

func (r *PostgresCuration) RecordImports(ctx context.Context, a domain.Actor, id uuid.UUID, productIDs []uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.RecordListImports(ctx, dbgen.RecordListImportsParams{
			ListID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID, ProductIds: productIDs,
		})
	})
}

func (r *PostgresCuration) ImportersByProduct(ctx context.Context, a domain.Actor, id uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ListImportersByProduct(ctx, dbgen.ListImportersByProductParams{ListID: id, WorkspaceID: a.WorkspaceID})
		for _, row := range rows {
			out[row.ProductID] = row.Importers
		}
		return err
	})
	return out, err
}

func (r *PostgresCuration) Importers(ctx context.Context, a domain.Actor, id uuid.UUID) ([]domain.ListImporter, error) {
	var out []domain.ListImporter
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.ListImporters(ctx, dbgen.ListImportersParams{ListID: id, WorkspaceID: a.WorkspaceID})
		out = make([]domain.ListImporter, len(rows))
		for i, row := range rows {
			out[i] = domain.ListImporter{UserID: row.UserID, Products: row.Products, LastImportedAt: row.LastImportedAt}
		}
		return err
	})
	return out, err
}

func (r *PostgresCuration) PublishedImports(ctx context.Context, a domain.Actor) ([]domain.PublishedListImport, error) {
	var out []domain.PublishedListImport
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.PublishedListImports(ctx, a.WorkspaceID)
		out = make([]domain.PublishedListImport, len(rows))
		for i, row := range rows {
			out[i] = domain.PublishedListImport{ListID: row.ListID, ListImport: domain.ListImport{
				UserID: row.UserID, ProductID: row.ProductID, ImportedAt: row.ImportedAt,
			}}
		}
		return err
	})
	return out, err
}
