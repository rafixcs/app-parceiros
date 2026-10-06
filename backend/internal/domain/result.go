package domain

import (
	"context"
	"time"
	_ "time/tzdata" // the Brasília time zone even in images without tzdata

	"github.com/google/uuid"
)

const (
	// ConversionSyncWindow is how far back each sync reads: the maximum of
	// the conversion report (90 days), with a margin. It reads it all again
	// because the status of the orders changes after the purchase (pending,
	// completed, cancelled).
	ConversionSyncWindow = 89 * 24 * time.Hour
	// MinSyncInterval between two syncs requested by the user.
	MinSyncInterval = 10 * time.Minute
	// MaxPeriodDays is the longest period of a dashboard query.
	MaxPeriodDays = 366
	// DefaultPeriodDays is the period when the query gives none.
	DefaultPeriodDays = 30
)

// ResultsLocation is the time zone of the days in the dashboards.
var ResultsLocation = mustLoadLocation("America/Sao_Paulo")

func mustLoadLocation(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// SyncStatus is the state of a user's conversion sync.
type SyncStatus string

const (
	SyncNever        SyncStatus = "never"
	SyncSyncing      SyncStatus = "syncing"
	SyncOK           SyncStatus = "ok"
	SyncError        SyncStatus = "error"
	SyncNoCredential SyncStatus = "no_credential"
)

// ConversionSync is the state of the user's sync. FinishedAt and Conversions
// are those of the last successful one. ErrorCode is one of the codes of
// ErrSyncCredentialRefused or ErrSyncUnavailable: fixed, because the original
// error may hold details of the call that must not reach the user.
type ConversionSync struct {
	Status      SyncStatus
	RequestedAt *time.Time
	FinishedAt  *time.Time
	Conversions int32
	ErrorCode   *string
}

// Period is a range of whole days in ResultsLocation: [Start, End).
type Period struct {
	From  string // YYYY-MM-DD
	To    string // YYYY-MM-DD, inclusive
	Start time.Time
	End   time.Time
}

// NewPeriod reads from and to (YYYY-MM-DD, inclusive). Empty values mean the
// last DefaultPeriodDays days up to today. It returns ErrInvalidPeriod for
// malformed dates, from after to, or more than MaxPeriodDays days.
func NewPeriod(from, to string, now time.Time) (Period, error) {
	today := now.In(ResultsLocation)
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, ResultsLocation)
	if to != "" {
		t, err := time.ParseInLocation(time.DateOnly, to, ResultsLocation)
		if err != nil {
			return Period{}, ErrInvalidPeriod
		}
		end = t
	}
	start := end.AddDate(0, 0, -(DefaultPeriodDays - 1))
	if from != "" {
		t, err := time.ParseInLocation(time.DateOnly, from, ResultsLocation)
		if err != nil {
			return Period{}, ErrInvalidPeriod
		}
		start = t
	}
	if start.After(end) || end.Sub(start) >= MaxPeriodDays*24*time.Hour {
		return Period{}, ErrInvalidPeriod
	}
	return Period{
		From: start.Format(time.DateOnly), To: end.Format(time.DateOnly),
		Start: start, End: end.AddDate(0, 0, 1),
	}, nil
}

// ResultTotals sums the conversions of a period. Orders and amounts leave out
// the cancelled ones; the validated commission is that of completed orders.
type ResultTotals struct {
	Orders                   int64
	Cancelled                int64
	Items                    int64
	SalesCents               int64
	EstimatedCommissionCents int64
	ValidatedCommissionCents int64
}

type DayResult struct {
	Day                      string // YYYY-MM-DD in ResultsLocation
	Orders                   int64
	EstimatedCommissionCents int64
	ValidatedCommissionCents int64
}

type ProductResult struct {
	ItemID int64
	// ProductID and ImageURL come from the catalog, when the item is in it.
	ProductID                *uuid.UUID
	Name                     string
	ShopName                 string
	ImageURL                 *string
	Orders                   int64
	Items                    int64
	SalesCents               int64
	EstimatedCommissionCents int64
	ValidatedCommissionCents int64
}

type ChannelResult struct {
	// Channel of the link; empty when the sale came from a link made outside
	// the app.
	Channel                  Channel
	Orders                   int64
	EstimatedCommissionCents int64
	ValidatedCommissionCents int64
}

// MyResults are the user's own results in the workspace.
type MyResults struct {
	Period Period
	Sync   ConversionSync
	// SharesResults only exists in mentorships: whether the user shows the
	// results to the mentor.
	SharesResults *bool
	Totals        ResultTotals
	ByDay         []DayResult
	ByProduct     []ProductResult
	ByChannel     []ChannelResult
}

// ListResult sums the sales of the products of a curated list, by the
// consenting affiliates who imported them, after the import.
type ListResult struct {
	ID                       uuid.UUID
	Title                    string
	PublishedAt              time.Time
	Importers                int
	Orders                   int64
	SalesCents               int64
	EstimatedCommissionCents int64
	ValidatedCommissionCents int64
}

// GroupResults are the aggregated results of the members who consented.
// Never per affiliate.
type GroupResults struct {
	Period Period
	// Affiliates is the number of affiliates; Sharing, how many members
	// (affiliates or not) consent; Active, how many of those sold in the
	// period.
	Affiliates int
	Sharing    int
	Active     int64
	Totals     ResultTotals
	ByDay      []DayResult
	ByList     []ListResult
	ByProduct  []ProductResult
}

// ImportedList is a list published to the group, with the imports of its
// products (curation).
type ImportedList struct {
	ID          uuid.UUID
	Title       string
	PublishedAt *time.Time
	Imports     []ListImport
}

// ListImport is a product of a list imported by a member.
type ListImport struct {
	UserID     uuid.UUID
	ProductID  uuid.UUID
	ImportedAt time.Time
}

// StoredConversion is a conversion of the report placed in a workspace.
type StoredConversion struct {
	Conversion
	WorkspaceID uuid.UUID
	ProductID   *uuid.UUID
	Channel     *Channel
}

// ResultQuery selects the conversions of some users in a workspace and
// period.
type ResultQuery struct {
	WorkspaceID uuid.UUID
	UserIDs     []uuid.UUID
	Start, End  time.Time
}

// ResultImport is one product imported by a user since a moment, counted
// under the list Group (an index).
type ResultImport struct {
	Group     int
	UserID    uuid.UUID
	ProductID uuid.UUID
	Since     time.Time
}

// ResultRepository keeps the conversions and the sync state. Reads act for
// the actor (RLS): the user sees their own conversions, and owner and mentor
// those of members who consented. Only the sync (a user without workspace)
// writes.
type ResultRepository interface {
	// SaveConversions upserts the user's conversions, in one transaction.
	SaveConversions(ctx context.Context, userID uuid.UUID, cs []StoredConversion) error
	// Totals returns the totals and how many users sold.
	Totals(ctx context.Context, a Actor, q ResultQuery) (ResultTotals, int64, error)
	ByDay(ctx context.Context, a Actor, q ResultQuery) ([]DayResult, error)
	// ByProduct returns the best products, by estimated commission, without
	// the catalog image.
	ByProduct(ctx context.Context, a Actor, q ResultQuery, limit int) ([]ProductResult, error)
	ByChannel(ctx context.Context, a Actor, q ResultQuery) ([]ChannelResult, error)
	// ByImportGroup sums the sales of each import, by group. Only the groups
	// with sales are in the map.
	ByImportGroup(ctx context.Context, a Actor, q ResultQuery, imports []ResultImport) (map[int]ListResult, error)

	// ConversionSync returns ErrNotFound when the user never synced.
	ConversionSync(ctx context.Context, userID uuid.UUID) (ConversionSync, error)
	StartConversionSync(ctx context.Context, userID uuid.UUID, now time.Time) (ConversionSync, error)
	FinishConversionSync(ctx context.Context, userID uuid.UUID, status SyncStatus, now time.Time, conversions int, errorCode *string) error
}

// ConversionSyncQueue enqueues the sync_conversions job of a user.
type ConversionSyncQueue interface {
	EnqueueConversionSync(ctx context.Context, userID uuid.UUID) error
}

var (
	ErrGroupResultsForbidden  = NewError(KindForbidden, "group_results_forbidden")
	ErrGroupResultsMentorship = NewError(KindConflict, "group_results_mentorship_only")
	ErrSyncNoCredential       = NewError(KindConflict, "sync_no_credential")
	ErrSyncedRecently         = NewError(KindTooManyRequests, "synced_recently")
	ErrInvalidPeriod          = NewError(KindInvalid, "invalid_period")
	// Reasons of a failed sync, kept in ConversionSync.ErrorCode.
	ErrSyncCredentialRefused = NewError(KindUpstream, "sync_credential_refused")
	ErrSyncUnavailable       = NewError(KindUpstream, "sync_shopee_unavailable")
)
