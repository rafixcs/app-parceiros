package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// SnapshotInterval is how often each category of the catalog is collected.
const SnapshotInterval = 6 * time.Hour

// catalogPageSize is the most offers per page the Shopee Open API returns.
const catalogPageSize = 50

// ProductService keeps the global catalog: the periodic collection of the
// source with the app credential and the reads of the other modules.
type ProductService struct {
	repo domain.ProductRepository
	// catalog is the source with the app credential; nil when the server has
	// none (the catalog is not collected and pasted links are not imported).
	catalog domain.Catalog
	raw     domain.RawStore   // nil keeps no raw answers
	trends  domain.TrendQueue // nil computes no trends after a collection
	log     *slog.Logger
	now     func() time.Time
}

func NewProductService(repo domain.ProductRepository, catalog domain.Catalog, raw domain.RawStore, trends domain.TrendQueue, log *slog.Logger) *ProductService {
	return &ProductService{repo: repo, catalog: catalog, raw: raw, trends: trends, log: log, now: time.Now}
}

// Catalog is the source with the app credential, or nil.
func (s *ProductService) Catalog() domain.Catalog { return s.catalog }

// Snapshot collects a category (0 = all) of the catalog, page by page, up to
// `pages` pages, and records each page. The pagination is sequential and
// respects the rate limit. On domain.ErrSourceLimit the pages already
// recorded stay and the caller retries later. After a collection with
// products it schedules the trends.
func (s *ProductService) Snapshot(ctx context.Context, categoryID int64, pages int) (int, error) {
	if s.catalog == nil {
		return 0, errors.New("no catalog configured")
	}
	collectedAt := s.now().UTC().Truncate(time.Hour)
	pages = max(pages, 1)
	log := s.log.With("category", categoryID)

	total := 0
	for page := 1; page <= pages; page++ {
		p, err := s.catalog.Offers(ctx, domain.CatalogFilter{CategoryID: categoryID, Page: page, Limit: catalogPageSize})
		if err != nil {
			return total, fmt.Errorf("page %d: %w", page, err)
		}
		key := fmt.Sprintf("%s/catalog/%s/category-%d/page-%03d.json.gz",
			s.catalog.Source(), collectedAt.Format("2006/01/02/15"), categoryID, page)
		if err := s.keepRaw(ctx, key, p.Raw); err != nil {
			// The raw answer is for reprocessing; not worth losing the collection.
			log.Warn("could not keep the raw answer", "key", key, "err", err)
		}
		if err := s.repo.Record(ctx, s.catalog.Source(), collectedAt, p.Offers); err != nil {
			return total, fmt.Errorf("recording page %d: %w", page, err)
		}
		total += len(p.Offers)
		if !p.HasNext {
			break
		}
	}
	log.Info("catalog collected", "products", total)
	if total > 0 && s.trends != nil {
		return total, s.trends.EnqueueTrends(ctx)
	}
	return total, nil
}

func (s *ProductService) keepRaw(ctx context.Context, key string, raw []byte) error {
	if s.raw == nil || len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return s.raw.Put(ctx, key, buf.Bytes(), "application/gzip")
}

// MonitoredCategories lists the categories with a periodic snapshot.
func (s *ProductService) MonitoredCategories(ctx context.Context, source domain.Source) ([]int64, error) {
	return s.repo.MonitoredCategories(ctx, source)
}

// SaveCategories registers or updates categories (worker).
func (s *ProductService) SaveCategories(ctx context.Context, source domain.Source, cs []domain.Category) error {
	return s.repo.SaveCategories(ctx, source, cs)
}

// Product reads a product of the catalog.
func (s *ProductService) Product(ctx context.Context, id uuid.UUID) (domain.Product, error) {
	p, err := s.repo.Product(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Product{}, domain.ErrProductNotFound
	}
	return p, err
}

// Products reads several products at once. Unknown ids are left out.
func (s *ProductService) Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error) {
	return s.repo.Products(ctx, ids)
}

// ProductByItem finds a product by its id at the source. It returns
// domain.ErrProductNotFound while it was not collected.
func (s *ProductService) ProductByItem(ctx context.Context, source domain.Source, itemID int64) (domain.Product, error) {
	p, err := s.repo.ProductByItem(ctx, source, itemID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Product{}, domain.ErrProductNotFound
	}
	return p, err
}

// History returns the snapshots of the product since `since`, oldest first.
func (s *ProductService) History(ctx context.Context, id uuid.UUID, since time.Time) ([]domain.Snapshot, error) {
	return s.repo.History(ctx, id, since)
}

// CategoryNames returns the names of the known categories among ids.
func (s *ProductService) CategoryNames(ctx context.Context, source domain.Source, ids []int64) (map[int64]string, error) {
	return s.repo.CategoryNames(ctx, source, ids)
}

// Import records in the catalog a product the user brought by pasting its
// link, fetched from the source with the app credential. It is the only write
// of the API in the catalog.
func (s *ProductService) Import(ctx context.Context, source domain.Source, o domain.Offer) (domain.Product, error) {
	if err := s.repo.Record(ctx, source, s.now(), []domain.Offer{o}); err != nil {
		return domain.Product{}, err
	}
	return s.ProductByItem(ctx, source, o.ItemID)
}

// SetClock replaces the clock (tests).
func (s *ProductService) SetClock(now func() time.Time) { s.now = now }
