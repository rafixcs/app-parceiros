package shopee

import (
	"context"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// AppCatalog binds the Client to the app credential for the global catalog
// (domain.Catalog).
type AppCatalog struct {
	Client     *Client
	Credential Credential
}

var _ domain.Catalog = AppCatalog{}

func (c AppCatalog) Source() domain.Source { return domain.SourceShopee }

func (c AppCatalog) Offers(ctx context.Context, f domain.CatalogFilter) (domain.CatalogPage, error) {
	return c.Client.Offers(ctx, c.Credential, OfferFilter{
		CategoryID: f.CategoryID, Sort: SortBestSellers, Page: f.Page, Limit: f.Limit,
	})
}

func (c AppCatalog) OfferByItem(ctx context.Context, itemID int64) (domain.Offer, error) {
	return c.Client.OfferByItem(ctx, c.Credential, itemID)
}
