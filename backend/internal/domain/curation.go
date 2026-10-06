package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Limits of a curated list, as the tables enforce them.
const (
	// MaxListItems is the most products a curated list holds.
	MaxListItems       = 100
	MaxListTitle       = 120
	MaxListDescription = 2000
	MaxListComment     = 1000
)

// NoticeListPublished is the notification kind of a published list.
const NoticeListPublished = "list_published"

// CuratedList is a list of products a mentor builds for the group. Drafts
// (PublishedAt nil) only show to owner and mentors.
type CuratedList struct {
	ID          uuid.UUID
	Title       string
	Description string
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Products is how many products the list has.
	Products int64
	// Importers is how many members imported something from the list; only
	// owner and mentors see it (nil for the others).
	Importers *int64
	// ImportedByMe says whether the viewer imported something from the list.
	ImportedByMe bool
}

// CuratedListItem is a product of a list, as stored.
type CuratedListItem struct {
	ProductID uuid.UUID
	Comment   string
	Position  int32
}

// ListItemView is a product of a list as a member sees it.
type ListItemView struct {
	Product  Product
	Comment  string
	Position int32
	// Importers is how many members imported the product from the list; only
	// owner and mentors see it.
	Importers *int64
	// MyItem is the product already saved in the viewer's collection, with
	// their affiliate links.
	MyItem *Item
	// Videos are the videos of the product the viewer sees: theirs and the
	// shared ones of the workspace.
	Videos []VideoView
}

// CuratedListDetail is a list with its products and the videos the mentor
// attached to it.
type CuratedListDetail struct {
	CuratedList
	Items  []ListItemView
	Videos []VideoView
}

// ListImporter is a member who imported a list: how many products and when
// they last imported.
type ListImporter struct {
	UserID         uuid.UUID
	Name           string
	Products       int64
	LastImportedAt time.Time
}

// ListDashboard shows the mentor who of the group imported a list.
type ListDashboard struct {
	// Affiliates is the size of the group (members with the affiliate role).
	Affiliates int
	Importers  []ListImporter
}

// PublishedListImport is a ListImport of one of the published lists.
type PublishedListImport struct {
	ListID uuid.UUID
	ListImport
}

// CurationRepository stores the curated lists, their products and the
// imports. Every call acts as the member `a`: drafts only show to owner and
// mentors, and only they write lists; members see their own imports, owner
// and mentors the group's. Missing records return ErrNotFound.
type CurationRepository interface {
	CountLists(ctx context.Context, a Actor) (int64, error)
	CreateList(ctx context.Context, a Actor, title, description string) (uuid.UUID, error)
	// Lists returns the lists: drafts first (only with drafts), then the
	// published ones, the newest first. Importers is always filled.
	Lists(ctx context.Context, a Actor, drafts bool) ([]CuratedList, error)
	// List returns one list; a draft only with drafts.
	List(ctx context.Context, a Actor, id uuid.UUID, drafts bool) (CuratedList, error)
	// LockList locks the list until the end of the transaction.
	LockList(ctx context.Context, a Actor, id uuid.UUID) error
	// UpdateList changes title and description (nil fields stay).
	UpdateList(ctx context.Context, a Actor, id uuid.UUID, title, description *string) error
	// PublishList publishes a draft and says whether it was one.
	PublishList(ctx context.Context, a Actor, id uuid.UUID) (bool, error)
	DeleteList(ctx context.Context, a Actor, id uuid.UUID) error
	// TouchList marks the list as updated now.
	TouchList(ctx context.Context, a Actor, id uuid.UUID) error

	ListItems(ctx context.Context, a Actor, id uuid.UUID) ([]CuratedListItem, error)
	// AddListItem puts the product at the end of the list and says whether it
	// was added (false: it was already there).
	AddListItem(ctx context.Context, a Actor, id, productID uuid.UUID, comment string) (bool, error)
	CommentListItem(ctx context.Context, a Actor, id, productID uuid.UUID, comment string) error
	RemoveListItem(ctx context.Context, a Actor, id, productID uuid.UUID) error
	// ReorderListItems sets the positions in the order of productIDs.
	ReorderListItems(ctx context.Context, a Actor, id uuid.UUID, productIDs []uuid.UUID) error

	// RecordImports records that the actor imported the products of a
	// published list (again is not an error).
	RecordImports(ctx context.Context, a Actor, id uuid.UUID, productIDs []uuid.UUID) error
	ImportersByProduct(ctx context.Context, a Actor, id uuid.UUID) (map[uuid.UUID]int64, error)
	// Importers returns who imported the list, the latest first (Name empty).
	Importers(ctx context.Context, a Actor, id uuid.UUID) ([]ListImporter, error)
	// PublishedImports returns every import of the published lists.
	PublishedImports(ctx context.Context, a Actor) ([]PublishedListImport, error)
}

// Curation errors.
var (
	ErrListNotFound        = NewError(KindNotFound, "list_not_found")
	ErrListItemNotFound    = NewError(KindNotFound, "list_item_not_found")
	ErrVideoNotInList      = NewError(KindNotFound, "video_not_in_list")
	ErrListManagersOnly    = NewError(KindForbidden, "list_managers_only")
	ErrListsMentorshipOnly = NewError(KindConflict, "lists_mentorship_only")
	ErrListLimit           = NewError(KindConflict, "list_limit")
	ErrListEmpty           = NewError(KindConflict, "list_empty")
	ErrAlreadyInList       = NewError(KindConflict, "already_in_list")
	ErrListFull            = NewError(KindConflict, "list_full")
	ErrListNotPublished    = NewError(KindConflict, "list_not_published")
	ErrInvalidListTitle    = NewError(KindInvalid, "invalid_list_title")
	ErrInvalidListDesc     = NewError(KindInvalid, "invalid_list_description")
	ErrInvalidListComment  = NewError(KindInvalid, "invalid_list_comment")
	ErrListCommentRequired = NewError(KindInvalid, "list_comment_required")
	ErrInvalidListOrder    = NewError(KindInvalid, "invalid_list_order")
	ErrProductNotInList    = NewError(KindInvalid, "product_not_in_list")
)
