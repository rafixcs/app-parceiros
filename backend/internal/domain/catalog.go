package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Source is a marketplace the products come from. The MVP has only Shopee;
// TikTok Shop and others come in behind the same interfaces.
type Source string

const SourceShopee Source = "shopee"

// Offer is a product as the source reports it at one moment. Money in cents
// and commission in basis points (1% = 100).
type Offer struct {
	ItemID        int64
	ShopID        int64
	ShopName      string
	Name          string
	ImageURL      string
	URL           string  // product page, without an affiliate link
	Categories    []int64 // from the most general to the most specific
	MinPriceCents int64
	MaxPriceCents int64
	CommissionBP  int32
	Sales         int64
	Rating        *float64 // nil while the product has no reviews
}

type CatalogFilter struct {
	CategoryID int64 // 0 = all
	Page       int   // starts at 1
	Limit      int
}

type CatalogPage struct {
	Offers  []Offer
	HasNext bool
	// Raw is the original answer, kept to reprocess it if the parser changes.
	Raw []byte
}

// Catalog lists the offers of a source with the app credential.
type Catalog interface {
	Source() Source
	Offers(ctx context.Context, f CatalogFilter) (CatalogPage, error)
	// OfferByItem finds a product by its id at the source. It returns
	// ErrSourceNotFound when the source does not have it.
	OfferByItem(ctx context.Context, itemID int64) (Offer, error)
}

// Affiliator generates affiliate links with the credential of each user.
type Affiliator interface {
	// Connected says whether the user has a valid credential connected.
	Connected(ctx context.Context, userID uuid.UUID) (bool, error)
	// GenerateLink returns the short affiliate link to the page `origin`,
	// tagged with subIDs. It returns ErrNoCredential without a credential.
	GenerateLink(ctx context.Context, userID uuid.UUID, origin string, subIDs []string) (string, error)
}

// OrderStatus is the state of an order in the conversion report.
type OrderStatus string

const (
	OrderUnpaid    OrderStatus = "unpaid"
	OrderPending   OrderStatus = "pending"
	OrderCompleted OrderStatus = "completed"
	OrderCancelled OrderStatus = "cancelled"
)

// Conversion is one item of an order attributed to a link of the affiliate.
// Money in cents.
type Conversion struct {
	ConversionID    int64
	OrderID         string
	ItemID          int64
	ModelID         int64
	ItemName        string
	ShopName        string
	Quantity        int32
	PriceCents      int64 // unit price
	CommissionCents int64 // total commission of the item in the order
	Status          OrderStatus
	// SubID holds the subIds of the link as the source returns them (joined
	// by hyphens).
	SubID       string
	PurchasedAt time.Time
	ClickedAt   *time.Time
}

// ConversionReport reads the conversions of each user with their credential.
type ConversionReport interface {
	// Conversions returns the conversions purchased in [from, to). It returns
	// ErrNoCredential without a connected credential.
	Conversions(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]Conversion, error)
}

// Errors of the sources. They are infrastructure outcomes, not business
// errors: each service decides what they mean for the customer.
var (
	// ErrSourceLimit: the source, or our rate limit per credential, asked to
	// wait. The caller tries again later.
	ErrSourceLimit = errors.New("source rate limit")
	// ErrSourceInvalidCredential: the source refused the credential
	// (signature).
	ErrSourceInvalidCredential = errors.New("credential refused by the source")
	// ErrSourceAccessDenied: valid credential, but no access (blocked account,
	// revoked permission).
	ErrSourceAccessDenied = errors.New("access denied by the source")
	// ErrSourceUnavailable: error of the source or of the network.
	ErrSourceUnavailable = errors.New("source unavailable")
	// ErrSourceNotFound: the source does not have the product.
	ErrSourceNotFound = errors.New("product not found at the source")
	// ErrNoCredential: the user has no credential connected to the source.
	ErrNoCredential = errors.New("no credential connected to the source")
)

// Product is a product of the global catalog, with its latest data.
type Product struct {
	ID            uuid.UUID
	Source        Source
	ItemID        int64
	ShopName      string
	Name          string
	ImageURL      *string
	CategoryID    *int64
	Categories    []int64
	URL           string
	MinPriceCents int64
	MaxPriceCents int64
	CommissionBP  int32
	Sales         int64
	Rating        *float64
	CollectedAt   time.Time
}

// EarningsPerSale is the price times the commission, in cents, rounded.
func EarningsPerSale(priceCents int64, commissionBP int32) int64 {
	return (priceCents*int64(commissionBP) + 5000) / 10000
}

// EarningsPerSaleCents is what the affiliate earns on a sale at the minimum
// price.
func (p Product) EarningsPerSaleCents() int64 {
	return EarningsPerSale(p.MinPriceCents, p.CommissionBP)
}

// Snapshot is a product at one collection, for its history.
type Snapshot struct {
	CollectedAt   time.Time
	MinPriceCents int64
	MaxPriceCents int64
	CommissionBP  int32
	Sales         int64
	Rating        *float64
}

// ProductBaseline is the latest data of a product and the starting point to
// measure its sales growth (see ProductRepository.ForTrends).
type ProductBaseline struct {
	Product
	BaseSales       int64
	BaseCollectedAt time.Time
}

type Category struct {
	ID        int64
	Name      string
	Monitored bool
}

var ErrProductNotFound = NewError(KindNotFound, "product_not_found")

// ProductLink is a product page of a source, read from a link the user
// pasted.
type ProductLink struct {
	Source Source
	ShopID int64
	ItemID int64
}

// LinkParser reads product links of the sources (e.g. shopee.com.br/...).
type LinkParser interface {
	// ParseProductLink returns ErrShortLink for a short link (s.shopee.com.br)
	// and ErrInvalidProductLink for anything that is not a product page.
	ParseProductLink(raw string) (ProductLink, error)
}

var (
	ErrInvalidProductLink = NewError(KindInvalid, "invalid_product_link")
	ErrShortLink          = NewError(KindInvalid, "short_link")
)

// ProductRepository keeps the global catalog. It is not customer data: the
// API only reads it, and the worker (and the import of a pasted link) writes
// it with the owner role of the tables.
type ProductRepository interface {
	// Record saves the offers of one collection: it updates the current data
	// of each product and appends a snapshot.
	Record(ctx context.Context, source Source, collectedAt time.Time, offers []Offer) error
	MonitoredCategories(ctx context.Context, source Source) ([]int64, error)
	SaveCategories(ctx context.Context, source Source, cs []Category) error
	CategoryNames(ctx context.Context, source Source, ids []int64) (map[int64]string, error)
	// ForTrends returns the products collected since `since`, with their
	// baseline.
	ForTrends(ctx context.Context, since time.Time) ([]ProductBaseline, error)
	Product(ctx context.Context, id uuid.UUID) (Product, error)
	Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Product, error)
	ProductByItem(ctx context.Context, source Source, itemID int64) (Product, error)
	History(ctx context.Context, id uuid.UUID, since time.Time) ([]Snapshot, error)
}

// RawStore keeps the raw answers of the sources (gzip), to reprocess them if
// the parser changes.
type RawStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
}
