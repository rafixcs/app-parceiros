package http

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// itemProductResponse is the summary of the product that goes with each
// saved item.
type itemProductResponse struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	ImageURL             *string   `json:"image_url"`
	ShopName             string    `json:"shop_name"`
	URL                  string    `json:"url"`
	MinPriceCents        int64     `json:"min_price_cents"`
	MaxPriceCents        int64     `json:"max_price_cents"`
	CommissionBP         int32     `json:"commission_bp"`
	EarningsPerSaleCents int64     `json:"earnings_per_sale_cents"`
	Sales                int64     `json:"sales"`
	Rating               *float64  `json:"rating"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func itemProductResponseOf(p domain.Product) itemProductResponse {
	return itemProductResponse{
		ID: p.ID, Name: p.Name, ImageURL: p.ImageURL, ShopName: p.ShopName, URL: p.URL,
		MinPriceCents: p.MinPriceCents, MaxPriceCents: p.MaxPriceCents, CommissionBP: p.CommissionBP,
		EarningsPerSaleCents: p.EarningsPerSaleCents(), Sales: p.Sales, Rating: p.Rating, UpdatedAt: p.CollectedAt,
	}
}

type channelLinkResponse struct {
	Channel domain.Channel `json:"channel"`
	SubID   string         `json:"sub_id"`
	URL     string         `json:"url"`
}

type itemResponse struct {
	ID            uuid.UUID             `json:"id"`
	Product       itemProductResponse   `json:"product"`
	Title         string                `json:"title"`
	Description   string                `json:"description"`
	Notes         string                `json:"notes"`
	Tags          []string              `json:"tags"`
	Status        domain.ItemStatus     `json:"status"`
	AffiliateLink *string               `json:"affiliate_link"`
	LinkOrigin    domain.LinkOrigin     `json:"link_origin"`
	LinkStatus    domain.LinkStatus     `json:"link_status"`
	Links         []channelLinkResponse `json:"links"`
	CollectionIDs []uuid.UUID           `json:"collection_ids"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

func itemResponseOf(it domain.Item) itemResponse {
	tags := it.Tags
	if tags == nil {
		tags = []string{}
	}
	collections := it.CollectionIDs
	if collections == nil {
		collections = []uuid.UUID{}
	}
	return itemResponse{
		ID: it.ID, Product: itemProductResponseOf(it.Product), Title: it.Title, Description: it.Description,
		Notes: it.Notes, Tags: tags, Status: it.Status, AffiliateLink: it.AffiliateLink,
		LinkOrigin: it.LinkOrigin, LinkStatus: it.LinkStatus,
		Links: mapSlice(it.Links, func(l domain.ChannelLink) channelLinkResponse {
			return channelLinkResponse{Channel: l.Channel, SubID: l.SubID, URL: l.URL}
		}),
		CollectionIDs: collections, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

type itemPageResponse struct {
	Items   []itemResponse `json:"items"`
	Total   int64          `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}

type collectionResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Items     int64     `json:"items"`
	CreatedAt time.Time `json:"created_at"`
}

func collectionResponseOf(c domain.Collection) collectionResponse {
	return collectionResponse{ID: c.ID, Name: c.Name, Items: c.Items, CreatedAt: c.CreatedAt}
}

type saveItemRequest struct {
	ProductID *uuid.UUID `json:"product_id"`
	URL       string     `json:"url"`
}

type updateItemRequest struct {
	Title       *string            `json:"title"`
	Description *string            `json:"description"`
	Notes       *string            `json:"notes"`
	Tags        *[]string          `json:"tags"`
	Status      *domain.ItemStatus `json:"status"`
	// AffiliateLink absent does not change; null goes back to the automatic
	// link; a string is a manual link.
	AffiliateLink json.RawMessage `json:"affiliate_link"`
}

type setCollectionsRequest struct {
	CollectionIDs []uuid.UUID `json:"collection_ids"`
}

type collectionNameRequest struct {
	Name string `json:"name"`
}

type pendingLinksResponse struct {
	Enqueued int `json:"enqueued"`
}
