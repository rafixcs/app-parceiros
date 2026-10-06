package domain

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ItemStatus is how the affiliate rates a saved product.
type ItemStatus string

const (
	ItemTesting   ItemStatus = "testing"
	ItemWinner    ItemStatus = "winner"
	ItemDiscarded ItemStatus = "discarded"
)

func (s ItemStatus) Valid() bool {
	return s == ItemTesting || s == ItemWinner || s == ItemDiscarded
}

// Channel is where the affiliate posts a link. Each channel gets its own
// automatic link; the one of ChannelOther is the item's main link.
type Channel string

const (
	ChannelInstagram Channel = "instagram"
	ChannelTikTok    Channel = "tiktok"
	ChannelWhatsApp  Channel = "whatsapp"
	ChannelOther     Channel = "other"
)

// Channels receive one automatic link each.
var Channels = []Channel{ChannelInstagram, ChannelTikTok, ChannelWhatsApp, ChannelOther}

// LinkOrigin tells the automatic link (generate_affiliate_link job) from one
// the user typed.
type LinkOrigin string

const (
	LinkAuto   LinkOrigin = "auto"
	LinkManual LinkOrigin = "manual"
)

// LinkStatus of the affiliate link of an item.
type LinkStatus string

const (
	LinkPending    LinkStatus = "pending" // no Shopee credential
	LinkGenerating LinkStatus = "generating"
	LinkReady      LinkStatus = "ready"
	LinkFailed     LinkStatus = "failed"
)

// WorkspaceMark is the subId that identifies the workspace in the links: "w"
// and the first 12 hex digits of its id. The conversion report uses it to put
// each sale in the workspace where the link was generated.
func WorkspaceMark(workspaceID uuid.UUID) string {
	ws := strings.ReplaceAll(workspaceID.String(), "-", "")
	return "w" + ws[:12]
}

// IsWorkspaceMark says whether the subId has the format of WorkspaceMark.
func IsWorkspaceMark(s string) bool {
	if len(s) != 13 || s[0] != 'w' {
		return false
	}
	for _, r := range s[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ChannelSubIDs tags the link with the channel and the workspace, so the
// conversion report can split the results.
func ChannelSubIDs(c Channel, workspaceID uuid.UUID) []string {
	return []string{string(c), WorkspaceMark(workspaceID)}
}

// JoinSubIDs keeps the subIds as the source returns them in the conversion
// report (utmContent): joined by hyphens.
func JoinSubIDs(s []string) string { return strings.Join(s, "-") }

// ChannelLink is the automatic link of an item for one channel.
type ChannelLink struct {
	Channel Channel
	SubID   string
	URL     string
}

// SavedItem is a product the user saved to promote, as stored.
type SavedItem struct {
	ID            uuid.UUID
	ProductID     uuid.UUID
	Title         string
	Description   string
	Notes         string
	Tags          []string
	Status        ItemStatus
	AffiliateLink *string
	LinkOrigin    LinkOrigin
	LinkStatus    LinkStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Item is a saved item with its product, links per channel and collections.
type Item struct {
	SavedItem
	Product       Product
	Links         []ChannelLink
	CollectionIDs []uuid.UUID
}

// ItemPage is a page of the user's items.
type ItemPage struct {
	Items   []Item
	Total   int64
	Page    int
	PerPage int
}

// Collection is a folder of saved items, with how many it has.
type Collection struct {
	ID        uuid.UUID
	Name      string
	Items     int64
	CreatedAt time.Time
}

// ItemFilter selects the items of a page. Zero values do not filter.
type ItemFilter struct {
	Query        string
	Status       ItemStatus
	CollectionID *uuid.UUID
	Tag          string
	Page         int
	PerPage      int
}

// ItemUpdate is a partial change of an item; nil fields stay. With
// ChangeLink, a nil Link goes back to the automatic link and a non-nil one is
// a manual link.
type ItemUpdate struct {
	Title       *string
	Description *string
	Notes       *string
	Tags        *[]string
	Status      *ItemStatus
	ChangeLink  bool
	Link        *string
}

// ImportedItem is a product that enters the collection from a curated list,
// with the mentor's comment.
type ImportedItem struct {
	ProductID uuid.UUID
	Comment   string
}

// ImportResult says how many items were created and how many were already
// saved.
type ImportResult struct {
	Created      int
	AlreadySaved int
	CollectionID *uuid.UUID
	// LinkStatus of the new items: generating with Shopee connected, pending
	// without, failed when the queue refused the jobs.
	LinkStatus LinkStatus
}

// NewSavedItem is an item to create.
type NewSavedItem struct {
	ProductID  uuid.UUID
	Title      string
	Notes      string
	LinkStatus LinkStatus
}

// ItemFields are the plain fields of an ItemUpdate, already validated. Nil
// fields stay.
type ItemFields struct {
	Title       *string
	Description *string
	Notes       *string
	Tags        []string
	Status      *ItemStatus
}

// ItemQuery is a validated ItemFilter, as the repository takes it. Query is
// a substring with the LIKE wildcards already escaped.
type ItemQuery struct {
	Query        string
	Status       ItemStatus
	CollectionID *uuid.UUID
	Tag          string
	Limit        int
	Offset       int
}

// CollectionRepository keeps the saved items of each user inside each
// workspace. Every call acts for the actor: other users (the mentor included)
// and other workspaces never see the rows. Missing records return ErrNotFound.
type CollectionRepository interface {
	// CreateItem creates the item, or returns the one already saved for the
	// product with created=false.
	CreateItem(ctx context.Context, a Actor, ni NewSavedItem) (SavedItem, bool, error)
	Item(ctx context.Context, a Actor, id uuid.UUID) (SavedItem, error)
	ItemsByProducts(ctx context.Context, a Actor, productIDs []uuid.UUID) ([]SavedItem, error)
	// ListItems returns a page, the newest first, and the total.
	ListItems(ctx context.Context, a Actor, q ItemQuery) ([]SavedItem, int64, error)
	UpdateItem(ctx context.Context, a Actor, id uuid.UUID, f ItemFields) (SavedItem, error)
	SetManualLink(ctx context.Context, a Actor, id uuid.UUID, link string) (SavedItem, error)
	// ResetAutoLink drops the current link and the links per channel, and
	// sets the automatic link with the status.
	ResetAutoLink(ctx context.Context, a Actor, id uuid.UUID, status LinkStatus) (SavedItem, error)
	// MarkLinkStatus changes the status of an automatic link only.
	MarkLinkStatus(ctx context.Context, a Actor, id uuid.UUID, status LinkStatus) error
	// CompleteAutoLink records the links per channel and the main link, unless
	// the user switched to a manual link meanwhile.
	CompleteAutoLink(ctx context.Context, a Actor, id uuid.UUID, links []ChannelLink, main string) error
	// ClaimPendingLinks marks as generating the automatic links pending or
	// failed, and returns their items.
	ClaimPendingLinks(ctx context.Context, a Actor) ([]uuid.UUID, error)
	DeleteItem(ctx context.Context, a Actor, id uuid.UUID) error
	SavedProductIDs(ctx context.Context, a Actor) ([]uuid.UUID, error)
	ItemLinks(ctx context.Context, a Actor, itemIDs []uuid.UUID) (map[uuid.UUID][]ChannelLink, error)
	ItemCollections(ctx context.Context, a Actor, itemIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)

	Collections(ctx context.Context, a Actor) ([]Collection, error)
	Collection(ctx context.Context, a Actor, id uuid.UUID) (Collection, error)
	CollectionByName(ctx context.Context, a Actor, name string) (uuid.UUID, error)
	// CreateCollection returns ErrCollectionExists for a name already used
	// (case-insensitive).
	CreateCollection(ctx context.Context, a Actor, name string) (uuid.UUID, error)
	RenameCollection(ctx context.Context, a Actor, id uuid.UUID, name string) error
	DeleteCollection(ctx context.Context, a Actor, id uuid.UUID) error
	// CountCollections counts how many of ids are collections of the actor.
	CountCollections(ctx context.Context, a Actor, ids []uuid.UUID) (int64, error)
	// SetItemCollections replaces the collections of the item.
	SetItemCollections(ctx context.Context, a Actor, itemID uuid.UUID, collectionIDs []uuid.UUID) error
	AddToCollection(ctx context.Context, a Actor, collectionID, itemID uuid.UUID) error
}

// AffiliateLinkJob asks for the links of an item, with its owner's
// credential.
type AffiliateLinkJob struct {
	ItemID      uuid.UUID
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
}

// AffiliateLinkQueue enqueues the generate_affiliate_link job.
type AffiliateLinkQueue interface {
	EnqueueAffiliateLinks(ctx context.Context, jobs ...AffiliateLinkJob) error
}

var (
	ErrItemNotFound          = NewError(KindNotFound, "item_not_found")
	ErrCollectionNotFound    = NewError(KindNotFound, "collection_not_found")
	ErrCollectionExists      = NewError(KindConflict, "collection_exists")
	ErrLinkNoCredential      = NewError(KindConflict, "no_credential")
	ErrImportUnavailable     = NewError(KindUnavailable, "import_unavailable")
	ErrLinkRateLimited       = NewError(KindTooManyRequests, "link_rate_limited")
	ErrProductLookupFailed   = NewError(KindUpstream, "product_lookup_failed")
	ErrProductOrLinkRequired = NewError(KindInvalid, "product_or_link_required")
	ErrInvalidItemTitle      = NewError(KindInvalid, "invalid_item_title")
	ErrInvalidItemDesc       = NewError(KindInvalid, "invalid_item_description")
	ErrInvalidItemNotes      = NewError(KindInvalid, "invalid_item_notes")
	ErrInvalidTag            = NewError(KindInvalid, "invalid_tag")
	ErrTooManyTags           = NewError(KindInvalid, "too_many_tags")
	ErrInvalidItemStatus     = NewError(KindInvalid, "invalid_item_status")
	ErrInvalidAffiliateLink  = NewError(KindInvalid, "invalid_affiliate_link")
	ErrInvalidCollectionName = NewError(KindInvalid, "invalid_collection_name")
	ErrTooManyCollections    = NewError(KindInvalid, "too_many_item_collections")
	ErrInvalidItemPage       = NewError(KindInvalid, "invalid_item_page")
)
