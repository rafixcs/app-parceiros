package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// maxResultProducts is how many products the dashboards list.
const maxResultProducts = 50

// resultsAccounts is what the results need from the accounts
// (AccountService).
type resultsAccounts interface {
	Workspaces(ctx context.Context, userID uuid.UUID) ([]domain.WorkspaceView, error)
	Members(ctx context.Context, m domain.Member) ([]domain.MemberDetail, error)
	SetSharesResults(ctx context.Context, m domain.Member, shares bool) error
}

// resultsLists gives the lists published to the group, with their imports
// (the curation service). Only owner and mentor call it.
type resultsLists interface {
	Imports(ctx context.Context, m domain.Member) ([]domain.ImportedList, error)
}

// resultsProducts reads the catalog (ProductService).
type resultsProducts interface {
	Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error)
	ProductByItem(ctx context.Context, source domain.Source, itemID int64) (domain.Product, error)
}

// resultsConnection says whether the user has Shopee connected
// (domain.Affiliator).
type resultsConnection interface {
	Connected(ctx context.Context, userID uuid.UUID) (bool, error)
}

// ResultService syncs each affiliate's conversions (orders and commission,
// from the Shopee conversion report with their own credential) and builds the
// dashboards: the affiliate's, with their own numbers, and the group's, with
// the aggregated numbers of those who consented, per list and per product.
//
// A conversion belongs to the user and lands in the workspace marked in the
// link's subId (domain.WorkspaceMark); without a mark, in the personal
// workspace. The mentor never sees individual results: only sums of the
// members who consented (LGPD).
type ResultService struct {
	repo       domain.ResultRepository
	report     domain.ConversionReport
	connection resultsConnection
	products   resultsProducts
	accounts   resultsAccounts
	// lists may be nil: the group dashboard then has no lists.
	lists resultsLists
	// queue enqueues sync_conversions; nil in the worker.
	queue domain.ConversionSyncQueue
	log   *slog.Logger
	now   func() time.Time
}

func NewResultService(repo domain.ResultRepository, report domain.ConversionReport, connection resultsConnection,
	products resultsProducts, accounts resultsAccounts, lists resultsLists, queue domain.ConversionSyncQueue, log *slog.Logger,
) *ResultService {
	return &ResultService{
		repo: repo, report: report, connection: connection, products: products, accounts: accounts,
		lists: lists, queue: queue, log: log, now: time.Now,
	}
}

// SetClock replaces the clock (tests).
func (s *ResultService) SetClock(now func() time.Time) { s.now = now }

// Mine returns the user's results in the member's workspace, in the period
// from-to (YYYY-MM-DD; empty for the last 30 days).
func (s *ResultService) Mine(ctx context.Context, m domain.Member, from, to string) (domain.MyResults, error) {
	p, err := domain.NewPeriod(from, to, s.now())
	if err != nil {
		return domain.MyResults{}, err
	}
	out := domain.MyResults{Period: p}
	if m.WorkspaceKind == domain.WorkspaceMentorship {
		shares := m.SharesResults
		out.SharesResults = &shares
	}
	if out.Sync, err = s.SyncStatus(ctx, m.UserID); err != nil {
		return domain.MyResults{}, err
	}
	a := m.Actor()
	q := domain.ResultQuery{WorkspaceID: m.WorkspaceID, UserIDs: []uuid.UUID{m.UserID}, Start: p.Start, End: p.End}
	if out.Totals, _, err = s.repo.Totals(ctx, a, q); err != nil {
		return domain.MyResults{}, err
	}
	if out.ByDay, err = s.repo.ByDay(ctx, a, q); err != nil {
		return domain.MyResults{}, err
	}
	if out.ByChannel, err = s.repo.ByChannel(ctx, a, q); err != nil {
		return domain.MyResults{}, err
	}
	if out.ByProduct, err = s.byProduct(ctx, a, q); err != nil {
		return domain.MyResults{}, err
	}
	return out, nil
}

// Group returns the aggregated results of the members who consented. Only
// owner and mentor, and only in mentorships. Never per affiliate.
func (s *ResultService) Group(ctx context.Context, m domain.Member, from, to string) (domain.GroupResults, error) {
	p, err := domain.NewPeriod(from, to, s.now())
	if err != nil {
		return domain.GroupResults{}, err
	}
	if m.WorkspaceKind != domain.WorkspaceMentorship {
		return domain.GroupResults{}, domain.ErrGroupResultsMentorship
	}
	if !m.Role.Manages() {
		return domain.GroupResults{}, domain.ErrGroupResultsForbidden
	}
	members, err := s.accounts.Members(ctx, m)
	if err != nil {
		return domain.GroupResults{}, err
	}
	out := domain.GroupResults{Period: p, ByList: []domain.ListResult{}}
	sharing := map[uuid.UUID]bool{}
	users := []uuid.UUID{}
	for _, mb := range members {
		if mb.Role == domain.RoleAffiliate {
			out.Affiliates++
		}
		if mb.SharesResults {
			sharing[mb.UserID] = true
			users = append(users, mb.UserID)
		}
	}
	out.Sharing = len(users)

	var lists []domain.ImportedList
	if s.lists != nil {
		if lists, err = s.lists.Imports(ctx, m); err != nil {
			return domain.GroupResults{}, err
		}
	}
	var imports []domain.ResultImport
	for i, l := range lists {
		importers := map[uuid.UUID]bool{}
		for _, imp := range l.Imports {
			importers[imp.UserID] = true
			if sharing[imp.UserID] {
				imports = append(imports, domain.ResultImport{
					Group: i, UserID: imp.UserID, ProductID: imp.ProductID, Since: imp.ImportedAt,
				})
			}
		}
		var published time.Time
		if l.PublishedAt != nil {
			published = *l.PublishedAt
		}
		out.ByList = append(out.ByList, domain.ListResult{
			ID: l.ID, Title: l.Title, PublishedAt: published, Importers: len(importers),
		})
	}

	a := m.Actor()
	q := domain.ResultQuery{WorkspaceID: m.WorkspaceID, UserIDs: users, Start: p.Start, End: p.End}
	if out.Totals, out.Active, err = s.repo.Totals(ctx, a, q); err != nil {
		return domain.GroupResults{}, err
	}
	if out.ByDay, err = s.repo.ByDay(ctx, a, q); err != nil {
		return domain.GroupResults{}, err
	}
	if out.ByProduct, err = s.byProduct(ctx, a, q); err != nil {
		return domain.GroupResults{}, err
	}
	byGroup, err := s.repo.ByImportGroup(ctx, a, q, imports)
	if err != nil {
		return domain.GroupResults{}, err
	}
	for i, g := range byGroup {
		l := &out.ByList[i]
		l.Orders, l.SalesCents = g.Orders, g.SalesCents
		l.EstimatedCommissionCents, l.ValidatedCommissionCents = g.EstimatedCommissionCents, g.ValidatedCommissionCents
	}
	return out, nil
}

// byProduct lists the best products, with the catalog image.
func (s *ResultService) byProduct(ctx context.Context, a domain.Actor, q domain.ResultQuery) ([]domain.ProductResult, error) {
	rows, err := s.repo.ByProduct(ctx, a, q, maxResultProducts)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for _, r := range rows {
		if r.ProductID != nil {
			ids = append(ids, *r.ProductID)
		}
	}
	if len(ids) == 0 {
		return rows, nil
	}
	catalog, err := s.products.Products(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ProductID != nil {
			if p, ok := catalog[*rows[i].ProductID]; ok {
				rows[i].ImageURL = p.ImageURL
			}
		}
	}
	return rows, nil
}

// SetConsent records whether the user shows their results to the group's
// managers (mentorships only).
func (s *ResultService) SetConsent(ctx context.Context, m domain.Member, shares bool) error {
	return s.accounts.SetSharesResults(ctx, m, shares)
}

// SyncStatus returns the state of the user's sync.
func (s *ResultService) SyncStatus(ctx context.Context, userID uuid.UUID) (domain.ConversionSync, error) {
	st, err := s.repo.ConversionSync(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ConversionSync{Status: domain.SyncNever}, nil
	}
	return st, err
}

// RequestSync enqueues the user's sync now. It refuses when they have no
// Shopee connected or the last one finished a moment ago. While one runs,
// it returns its state without enqueueing another.
func (s *ResultService) RequestSync(ctx context.Context, userID uuid.UUID) (domain.ConversionSync, error) {
	ok, err := s.connection.Connected(ctx, userID)
	if err != nil {
		return domain.ConversionSync{}, err
	}
	if !ok {
		return domain.ConversionSync{}, domain.ErrSyncNoCredential
	}
	cur, err := s.SyncStatus(ctx, userID)
	if err != nil {
		return domain.ConversionSync{}, err
	}
	now := s.now()
	if cur.Status == domain.SyncSyncing && cur.RequestedAt != nil && now.Sub(*cur.RequestedAt) < time.Hour {
		return cur, nil
	}
	if cur.Status == domain.SyncOK && cur.FinishedAt != nil && now.Sub(*cur.FinishedAt) < domain.MinSyncInterval {
		return domain.ConversionSync{}, domain.ErrSyncedRecently
	}
	if s.queue == nil {
		return domain.ConversionSync{}, errors.New("results: no sync queue")
	}
	out, err := s.repo.StartConversionSync(ctx, userID, now)
	if err != nil {
		return domain.ConversionSync{}, err
	}
	if err := s.queue.EnqueueConversionSync(ctx, userID); err != nil {
		return domain.ConversionSync{}, err
	}
	return out, nil
}

// Sync reads the conversions of the last domain.ConversionSyncWindow (the
// report pages in sequence) and saves them, each in the workspace marked in
// its subId or else in the personal one. It returns how many it read.
//
// Without a credential, it records no_credential. When Shopee refuses the
// credential, it records the error and returns nil (retrying will not help).
// A rate limit (domain.ErrSourceLimit) is returned as is, for the job to try
// again later; on other errors at the last attempt, it records the error.
func (s *ResultService) Sync(ctx context.Context, userID uuid.UUID, lastAttempt bool) (int, error) {
	end := s.now()
	convs, err := s.report.Conversions(ctx, userID, end.Add(-domain.ConversionSyncWindow), end)
	switch {
	case errors.Is(err, domain.ErrNoCredential):
		return 0, s.finish(ctx, userID, domain.SyncNoCredential, 0, nil)
	case errors.Is(err, domain.ErrSourceInvalidCredential), errors.Is(err, domain.ErrSourceAccessDenied):
		s.log.InfoContext(ctx, "credential refused; sync stopped", "user_id", userID, "err", err)
		return 0, s.finish(ctx, userID, domain.SyncError, 0, domain.ErrSyncCredentialRefused)
	case errors.Is(err, domain.ErrSourceLimit):
		return 0, err
	case err != nil:
		return 0, s.failed(ctx, userID, lastAttempt, err)
	}
	n, err := s.save(ctx, userID, convs)
	if err != nil {
		return 0, s.failed(ctx, userID, lastAttempt, err)
	}
	return n, s.finish(ctx, userID, domain.SyncOK, n, nil)
}

func (s *ResultService) save(ctx context.Context, userID uuid.UUID, convs []domain.Conversion) (int, error) {
	ws, err := s.accounts.Workspaces(ctx, userID)
	if err != nil {
		return 0, err
	}
	byMark := map[string]uuid.UUID{}
	var personal uuid.UUID
	for _, w := range ws {
		byMark[domain.WorkspaceMark(w.ID)] = w.ID
		if w.Kind == domain.WorkspacePersonal {
			personal = w.ID
		}
	}
	if personal == uuid.Nil {
		return 0, errors.New("results: user without a personal workspace")
	}

	catalog := map[int64]*uuid.UUID{}
	stored := make([]domain.StoredConversion, 0, len(convs))
	for _, c := range convs {
		if _, ok := catalog[c.ItemID]; !ok {
			p, err := s.products.ProductByItem(ctx, domain.SourceShopee, c.ItemID)
			switch {
			case errors.Is(err, domain.ErrProductNotFound):
				catalog[c.ItemID] = nil
			case err != nil:
				return 0, err
			default:
				catalog[c.ItemID] = &p.ID
			}
		}
		channel, mark := parseSubID(c.SubID)
		w, ok := byMark[mark]
		if !ok {
			w = personal
		}
		sc := domain.StoredConversion{Conversion: c, WorkspaceID: w, ProductID: catalog[c.ItemID]}
		if channel != "" {
			sc.Channel = &channel
		}
		stored = append(stored, sc)
	}
	if err := s.repo.SaveConversions(ctx, userID, stored); err != nil {
		return 0, err
	}
	return len(stored), nil
}

// failed records the sync as failed at the last attempt, and returns err.
func (s *ResultService) failed(ctx context.Context, userID uuid.UUID, lastAttempt bool, err error) error {
	if !lastAttempt {
		return err
	}
	s.log.ErrorContext(ctx, "conversion sync failed; giving up", "user_id", userID, "err", err)
	if errRec := s.finish(ctx, userID, domain.SyncError, 0, domain.ErrSyncUnavailable); errRec != nil {
		return errors.Join(err, errRec)
	}
	return err
}

func (s *ResultService) finish(ctx context.Context, userID uuid.UUID, st domain.SyncStatus, n int, reason *domain.Error) error {
	var code *string
	if reason != nil {
		c := reason.Code
		code = &c
	}
	return s.repo.FinishConversionSync(ctx, userID, st, s.now(), n, code)
}

// parseSubID finds the channel and the workspace mark in the subIds of the
// link (joined by hyphens, as the report returns them; empty subIds show up
// as consecutive hyphens).
func parseSubID(sub string) (channel domain.Channel, mark string) {
	for p := range strings.SplitSeq(sub, "-") {
		switch {
		case channel == "" && slices.Contains(domain.Channels, domain.Channel(p)):
			channel = domain.Channel(p)
		case mark == "" && domain.IsWorkspaceMark(p):
			mark = p
		}
	}
	return channel, mark
}
