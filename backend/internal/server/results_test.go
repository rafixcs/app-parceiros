package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

const (
	resAppIDAna  = "18300001234"
	resSecretAna = "s3cr3t-of-ana"
	resAppIDBia  = "18300005678"
	resSecretBia = "s3cr3t-of-bia"
	resInCatalog = 20
	resDaysAhead = 30
	// resPageSize is a small report page, to exercise the scrollId.
	resPageSize = 7
)

// resLinkQueue keeps the generate_affiliate_link jobs.
type resLinkQueue struct {
	mu   sync.Mutex
	jobs []domain.AffiliateLinkJob
}

func (q *resLinkQueue) EnqueueAffiliateLinks(_ context.Context, jobs ...domain.AffiliateLinkJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, jobs...)
	return nil
}

func (q *resLinkQueue) take() []domain.AffiliateLinkJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.jobs
	q.jobs = nil
	return out
}

// resSyncQueue keeps the sync_conversions jobs.
type resSyncQueue struct {
	mu    sync.Mutex
	users []uuid.UUID
}

func (q *resSyncQueue) EnqueueConversionSync(_ context.Context, userID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.users = append(q.users, userID)
	return nil
}

func (q *resSyncQueue) take() []uuid.UUID {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.users
	q.users = nil
	return out
}

// resLists stands for curation: the lists published to the group, with
// their imports.
type resLists struct {
	mu    sync.Mutex
	lists map[uuid.UUID][]domain.ImportedList
	// callers are the roles that asked for the lists.
	callers []domain.Role
}

func (l *resLists) Imports(_ context.Context, m domain.Member) ([]domain.ImportedList, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callers = append(l.callers, m.Role)
	return l.lists[m.WorkspaceID], nil
}

type resEnv struct {
	*testApp
	now    time.Time
	mock   *shopee.Mock
	client *shopee.Client
	links  *resLinkQueue
	syncs  *resSyncQueue
	lists  *resLists
	offers []domain.Offer
	worker *queue.SyncConversionsWorker
}

func newResEnv(t *testing.T) *resEnv {
	t.Helper()
	ctx := context.Background()
	// The clock of the results and of Shopee is in the future, so there are
	// sales after the imports made now.
	now := time.Now().Add(resDaysAhead * 24 * time.Hour)
	clock := func() time.Time { return now }
	e := &resEnv{
		now:   now,
		mock:  &shopee.Mock{Secrets: map[string]string{resAppIDAna: resSecretAna, resAppIDBia: resSecretBia}, Now: clock},
		links: &resLinkQueue{},
		syncs: &resSyncQueue{},
		lists: &resLists{lists: map[uuid.UUID][]domain.ImportedList{}},
	}
	e.client = shopee.NewMock(e.mock, shopee.Config{})
	app := shopee.AppCatalog{Client: e.client, Credential: shopee.Credential{AppID: "1", Secret: "mock"}}
	e.testApp = newTestApp(t, func(in *infra) {
		in.shopee = e.client
		in.catalog = app
		in.linkQueue = e.links
		in.conversionSyncQueue = e.syncs
		in.resultLists = e.lists
	})
	e.svcs.results.SetClock(clock)

	for page := 1; ; page++ {
		p, err := app.Offers(ctx, domain.CatalogFilter{Page: page, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		e.offers = append(e.offers, p.Offers...)
		if !p.HasNext {
			break
		}
	}
	if err := repository.NewPostgresProduct(e.pool).Record(ctx, domain.SourceShopee, time.Now(), e.offers[:resInCatalog]); err != nil {
		t.Fatal(err)
	}

	// The worker reads the report in small pages.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	report := shopee.Report{Credentials: e.svcs.shopeeCredentials, Client: e.client, Limit: resPageSize}
	svc := service.NewResultService(repository.NewPostgresResult(e.pool), report, e.svcs.affiliator,
		e.svcs.products, e.svcs.accounts, nil, nil, log)
	svc.SetClock(clock)
	e.worker = &queue.SyncConversionsWorker{Svc: svc, Log: log}
	return e
}

func (e *resEnv) connect(sub, appID, secret string) {
	e.t.Helper()
	if _, err := e.svcs.shopeeCredentials.Connect(context.Background(), e.me(sub).ID, appID, secret); err != nil {
		e.t.Fatal(err)
	}
}

func (e *resEnv) productID(i int) uuid.UUID {
	e.t.Helper()
	p, err := e.svcs.products.ProductByItem(context.Background(), domain.SourceShopee, e.offers[i].ItemID)
	if err != nil {
		e.t.Fatal(err)
	}
	return p.ID
}

func (e *resEnv) save(sub string, ws uuid.UUID, product int) {
	e.t.Helper()
	e.must(sub, http.MethodPost, wsPath(ws, "/items"), map[string]any{"product_id": e.productID(product)}, nil, http.StatusCreated)
}

// generateLinks runs generate_affiliate_link on the enqueued jobs: the mock
// remembers the links, and simulates sales through them.
func (e *resEnv) generateLinks() {
	e.t.Helper()
	w := &queue.GenerateAffiliateLinkWorker{Svc: e.svcs.collections, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, j := range e.links.take() {
		job := &river.Job[queue.GenerateAffiliateLinkArgs]{
			JobRow: &rivertype.JobRow{Kind: "generate_affiliate_link", Attempt: 1, MaxAttempts: 6},
			Args:   queue.GenerateAffiliateLinkArgs{ItemID: j.ItemID, WorkspaceID: j.WorkspaceID, UserID: j.UserID},
		}
		if err := w.Work(context.Background(), job); err != nil {
			e.t.Fatalf("generate_affiliate_link: %v", err)
		}
	}
}

// sync runs the sync_conversions worker for the user.
func (e *resEnv) sync(sub string) {
	e.t.Helper()
	job := &river.Job[queue.SyncConversionsArgs]{
		JobRow: &rivertype.JobRow{Kind: "sync_conversions", Attempt: 1, MaxAttempts: 5},
		Args:   queue.SyncConversionsArgs{UserID: e.me(sub).ID},
	}
	if err := e.worker.Work(context.Background(), job); err != nil {
		e.t.Fatalf("sync_conversions: %v", err)
	}
}

// expected sums the Shopee report as the Shopee dashboard would: orders not
// cancelled, estimated commission (not cancelled) and validated (completed),
// of the conversions that pass keep.
func (e *resEnv) expected(appID, secret string, keep func(domain.Conversion) bool) resTotalsJSON {
	e.t.Helper()
	cred := shopee.Credential{AppID: appID, Secret: secret}
	var convs []domain.Conversion
	f := shopee.ConversionFilter{From: e.now.Add(-domain.ConversionSyncWindow), To: e.now, Limit: resPageSize}
	for {
		p, err := e.client.Conversions(context.Background(), cred, f)
		if err != nil {
			e.t.Fatal(err)
		}
		convs = append(convs, p.Conversions...)
		if !p.HasNext {
			break
		}
		f.ScrollID = p.ScrollID
	}
	var t resTotalsJSON
	orders, cancelled := map[string]bool{}, map[string]bool{}
	for _, c := range convs {
		if !keep(c) {
			continue
		}
		if c.Status == domain.OrderCancelled {
			cancelled[c.OrderID] = true
			continue
		}
		orders[c.OrderID] = true
		t.Items += int64(c.Quantity)
		t.SalesCents += c.PriceCents * int64(c.Quantity)
		t.EstimatedCommissionCents += c.CommissionCents
		if c.Status == domain.OrderCompleted {
			t.ValidatedCommissionCents += c.CommissionCents
		}
	}
	t.Orders, t.Cancelled = int64(len(orders)), int64(len(cancelled))
	return t
}

// wholeWindow covers the whole sync window.
func (e *resEnv) wholeWindow() string {
	to := e.now.In(domain.ResultsLocation)
	from := e.now.Add(-domain.ConversionSyncWindow).In(domain.ResultsLocation)
	return "?from=" + from.Format(time.DateOnly) + "&to=" + to.Format(time.DateOnly)
}

func hasMark(ws uuid.UUID) func(domain.Conversion) bool {
	mark := domain.WorkspaceMark(ws)
	return func(c domain.Conversion) bool { return strings.Contains(c.SubID, mark) }
}

// JSON of the results routes, as clients see it.

type resTotalsJSON struct {
	Orders                   int64 `json:"orders"`
	Cancelled                int64 `json:"cancelled"`
	Items                    int64 `json:"items"`
	SalesCents               int64 `json:"sales_cents"`
	EstimatedCommissionCents int64 `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64 `json:"validated_commission_cents"`
}

type resSyncJSON struct {
	Status      string     `json:"status"`
	RequestedAt *time.Time `json:"requested_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Conversions int32      `json:"conversions"`
	ErrorCode   *string    `json:"error_code"`
	Error       *string    `json:"error"`
}

type resDayJSON struct {
	Day                      string `json:"day"`
	Orders                   int64  `json:"orders"`
	EstimatedCommissionCents int64  `json:"estimated_commission_cents"`
}

type resProductJSON struct {
	ItemID                   int64      `json:"item_id"`
	ProductID                *uuid.UUID `json:"product_id"`
	Name                     string     `json:"name"`
	ImageURL                 *string    `json:"image_url"`
	EstimatedCommissionCents int64      `json:"estimated_commission_cents"`
}

type resChannelJSON struct {
	Channel                  string `json:"channel"`
	EstimatedCommissionCents int64  `json:"estimated_commission_cents"`
}

type resMineJSON struct {
	Period struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"period"`
	Sync          resSyncJSON      `json:"sync"`
	SharesResults *bool            `json:"shares_results"`
	Totals        resTotalsJSON    `json:"totals"`
	ByDay         []resDayJSON     `json:"by_day"`
	ByProduct     []resProductJSON `json:"by_product"`
	ByChannel     []resChannelJSON `json:"by_channel"`
}

type resListJSON struct {
	ID                       uuid.UUID `json:"id"`
	Title                    string    `json:"title"`
	Importers                int       `json:"importers"`
	Orders                   int64     `json:"orders"`
	EstimatedCommissionCents int64     `json:"estimated_commission_cents"`
}

type resGroupJSON struct {
	Affiliates int              `json:"affiliates"`
	Sharing    int              `json:"sharing"`
	Active     int64            `json:"active"`
	Totals     resTotalsJSON    `json:"totals"`
	ByDay      []resDayJSON     `json:"by_day"`
	ByList     []resListJSON    `json:"by_list"`
	ByProduct  []resProductJSON `json:"by_product"`
}

// TestResultsMatchShopee is the M6 criterion: the dashboard numbers match the
// Shopee report, split by workspace, and the mentor only sees the sum of
// those who consented.
func TestResultsMatchShopee(t *testing.T) {
	e := newResEnv(t)
	ws := e.mentorship("mentor", "Turma").ID
	ana := e.join("mentor", "ana", ws).ID
	e.join("mentor", "bia", ws)
	personalAna := e.personal("ana").ID
	e.connect("ana", resAppIDAna, resSecretAna)
	e.connect("bia", resAppIDBia, resSecretBia)

	// The mentor published a list with two products, which Ana imported
	// (curation is a fake here: Ana saves them herself). Ana also saves a
	// product in the personal workspace, and Bia one in the mentorship.
	importedAt := time.Now()
	published := importedAt.Add(-time.Hour)
	list := domain.ImportedList{ID: uuid.New(), Title: "Achados", PublishedAt: &published, Imports: []domain.ListImport{
		{UserID: ana, ProductID: e.productID(0), ImportedAt: importedAt},
		{UserID: ana, ProductID: e.productID(1), ImportedAt: importedAt},
	}}
	e.lists.lists[ws] = []domain.ImportedList{list}
	e.save("ana", ws, 0)
	e.save("ana", ws, 1)
	e.save("ana", personalAna, 2)
	e.save("bia", ws, 3)
	e.generateLinks()

	// Before syncing: empty dashboard, sync "never".
	var mine resMineJSON
	e.must("ana", http.MethodGet, wsPath(ws, "/results"), nil, &mine, http.StatusOK)
	if mine.Sync.Status != "never" || mine.Totals.Orders != 0 || mine.SharesResults == nil || *mine.SharesResults ||
		mine.ByDay == nil || mine.ByProduct == nil || mine.ByChannel == nil {
		t.Fatalf("before syncing: %+v", mine)
	}

	e.sync("ana")
	e.sync("bia")
	e.sync("ana") // again: no duplicates

	whole := e.wholeWindow()
	want := e.expected(resAppIDAna, resSecretAna, hasMark(ws))
	e.must("ana", http.MethodGet, wsPath(ws, "/results"+whole), nil, &mine, http.StatusOK)
	if want.Orders == 0 || mine.Totals != want {
		t.Fatalf("Ana's mentorship: %+v, want %+v", mine.Totals, want)
	}
	if mine.Sync.Status != "ok" || mine.Sync.FinishedAt == nil || mine.Sync.Conversions == 0 {
		t.Fatalf("sync: %+v", mine.Sync)
	}
	var byDay, byChannel, byProduct int64
	for _, d := range mine.ByDay {
		byDay += d.EstimatedCommissionCents
	}
	for _, c := range mine.ByChannel {
		if c.Channel == "" {
			t.Fatalf("sale without channel in the mentorship: %+v", mine.ByChannel)
		}
		byChannel += c.EstimatedCommissionCents
	}
	for _, p := range mine.ByProduct {
		if p.ProductID == nil || p.ImageURL == nil {
			t.Fatalf("product without catalog: %+v", p)
		}
		byProduct += p.EstimatedCommissionCents
	}
	if byDay != want.EstimatedCommissionCents || byChannel != want.EstimatedCommissionCents || byProduct != want.EstimatedCommissionCents {
		t.Fatalf("sums by day %d, channel %d, product %d; want %d", byDay, byChannel, byProduct, want.EstimatedCommissionCents)
	}

	// In the personal workspace: everything without the mentorship mark
	// (links of the personal workspace and sales through links made outside
	// the app).
	var personal resMineJSON
	e.must("ana", http.MethodGet, wsPath(personalAna, "/results"+whole), nil, &personal, http.StatusOK)
	wantPersonal := e.expected(resAppIDAna, resSecretAna, func(c domain.Conversion) bool { return !hasMark(ws)(c) })
	if personal.Totals != wantPersonal || personal.SharesResults != nil {
		t.Fatalf("Ana's personal: %+v, want %+v", personal, wantPersonal)
	}

	// Without consent, the mentor sees zero; Bia sees neither the group nor
	// Ana.
	var group resGroupJSON
	e.must("mentor", http.MethodGet, wsPath(ws, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Affiliates != 2 || group.Sharing != 0 || group.Totals != (resTotalsJSON{}) || group.Active != 0 ||
		len(group.ByProduct) != 0 || len(group.ByList) != 1 || group.ByList[0].Orders != 0 ||
		group.ByList[0].Importers != 1 || group.ByList[0].Title != "Achados" {
		t.Fatalf("group without consent: %+v", group)
	}
	e.mustFail("bia", http.MethodGet, wsPath(ws, "/results/group"), nil, http.StatusForbidden, "group_results_forbidden")
	e.mustFail("mentor", http.MethodGet, wsPath(e.personal("mentor").ID, "/results/group"), nil,
		http.StatusConflict, "group_results_mentorship_only")
	e.mustFail("ana", http.MethodPut, wsPath(personalAna, "/results/consent"), map[string]any{"shares_results": true},
		http.StatusConflict, "consent_mentorship_only")
	for _, r := range e.lists.callers {
		if !r.Manages() {
			t.Fatalf("an affiliate read the lists: %v", e.lists.callers)
		}
	}
	var bia resMineJSON
	e.must("bia", http.MethodGet, wsPath(ws, "/results"+whole), nil, &bia, http.StatusOK)
	if bia.Totals != e.expected(resAppIDBia, resSecretBia, hasMark(ws)) || bia.Totals.Orders == 0 {
		t.Fatalf("Bia sees other numbers: %+v", bia.Totals)
	}

	// Ana consents: the mentor sees her sum, and the list counts only the
	// sales of the imported products, after the import.
	var c map[string]bool
	e.must("ana", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true}, &c, http.StatusOK)
	if !c["shares_results"] {
		t.Fatalf("consent: %+v", c)
	}
	var members []memberJSON
	e.must("mentor", http.MethodGet, wsPath(ws, "/members"), nil, &members, http.StatusOK)
	for _, m := range members {
		if m.SharesResults != (m.UserID == ana) {
			t.Fatalf("members: %+v", members)
		}
	}
	e.must("ana", http.MethodGet, wsPath(ws, "/results"), nil, &mine, http.StatusOK)
	if mine.SharesResults == nil || !*mine.SharesResults {
		t.Fatalf("Ana's consent in her dashboard: %+v", mine.SharesResults)
	}
	e.must("mentor", http.MethodGet, wsPath(ws, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Sharing != 1 || group.Totals != want || group.Active != 1 {
		t.Fatalf("group with Ana: %+v, want %+v", group, want)
	}
	inList := map[int64]bool{e.offers[0].ItemID: true, e.offers[1].ItemID: true}
	wantList := e.expected(resAppIDAna, resSecretAna, func(c domain.Conversion) bool {
		return hasMark(ws)(c) && inList[c.ItemID] && !c.PurchasedAt.Before(importedAt)
	})
	l := group.ByList[0]
	if l.ID != list.ID || l.Importers != 1 || l.Orders == 0 || l.Orders != wantList.Orders ||
		l.EstimatedCommissionCents != wantList.EstimatedCommissionCents {
		t.Fatalf("list: %+v, want %+v", l, wantList)
	}

	// Bia consents too: the sum of both.
	e.must("bia", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true}, nil, http.StatusOK)
	e.must("mentor", http.MethodGet, wsPath(ws, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Sharing != 2 || group.Active != 2 ||
		group.Totals.EstimatedCommissionCents != want.EstimatedCommissionCents+bia.Totals.EstimatedCommissionCents {
		t.Fatalf("group with both: %+v", group.Totals)
	}

	// Ana withdraws: she leaves the dashboard at once.
	e.must("ana", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": false}, nil, http.StatusOK)
	e.must("mentor", http.MethodGet, wsPath(ws, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Sharing != 1 || group.Totals != bia.Totals || group.ByList[0].Orders != 0 {
		t.Fatalf("after withdrawing: %+v, want %+v", group, bia.Totals)
	}
}

func TestResultsInvalidRequests(t *testing.T) {
	e := newResEnv(t)
	ws := e.mentorship("mentor", "Turma").ID
	for _, q := range []string{"?from=yesterday", "?from=2026-10-05&to=2026-10-01", "?from=2025-01-01&to=2026-10-01"} {
		e.mustFail("mentor", http.MethodGet, wsPath(ws, "/results"+q), nil, http.StatusUnprocessableEntity, "invalid_period")
		e.mustFail("mentor", http.MethodGet, wsPath(ws, "/results/group"+q), nil, http.StatusUnprocessableEntity, "invalid_period")
	}
	for _, body := range []string{`{}`, `{"shares_results":"yes"}`, `{"shares_results":true,"other":1}`, `{`} {
		if st, _ := e.rawCall("mentor", http.MethodPut, wsPath(ws, "/results/consent"), body); st != http.StatusBadRequest {
			t.Fatalf("consent %s: %d", body, st)
		}
	}
	e.mustFail("", http.MethodGet, "/v1/me/results/sync", nil, http.StatusUnauthorized, "unauthenticated")
	e.mustFail("", http.MethodGet, wsPath(ws, "/results"), nil, http.StatusUnauthorized, "unauthenticated")

	// Consent still works in a suspended workspace; the dashboards do not.
	e.join("mentor", "ana", ws)
	e.admin("UPDATE workspaces SET access_until = now() - interval '1 minute' WHERE id = $1", ws)
	e.mustFail("ana", http.MethodGet, wsPath(ws, "/results"), nil, http.StatusPaymentRequired, "workspace_suspended")
	e.must("ana", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true}, nil, http.StatusOK)
}

// TestResultsLeakAPI: no route shows the results of a workspace to whoever
// is not a member, and a member only sees the conversions of that workspace.
func TestResultsLeakAPI(t *testing.T) {
	e := newResEnv(t)
	ws := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", ws)
	other := e.mentorship("intruder", "Outra").ID
	e.join("intruder", "bia", other)
	e.connect("ana", resAppIDAna, resSecretAna)
	e.save("ana", ws, 0)
	e.generateLinks()
	e.sync("ana")
	e.must("ana", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true}, nil, http.StatusOK)

	whole := e.wholeWindow()
	for _, sub := range []string{"intruder", "bia"} {
		e.mustFail(sub, http.MethodGet, wsPath(ws, "/results"+whole), nil, http.StatusNotFound, "workspace_not_found")
		e.mustFail(sub, http.MethodGet, wsPath(ws, "/results/group"+whole), nil, http.StatusNotFound, "workspace_not_found")
		e.mustFail(sub, http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true},
			http.StatusNotFound, "workspace_not_found")
	}
	var group resGroupJSON
	e.must("intruder", http.MethodGet, wsPath(other, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Totals != (resTotalsJSON{}) || group.Sharing != 0 {
		t.Fatalf("the other mentorship sees: %+v", group)
	}
	var mine resMineJSON
	e.must("mentor", http.MethodGet, wsPath(ws, "/results"+whole), nil, &mine, http.StatusOK)
	if mine.Totals != (resTotalsJSON{}) {
		t.Fatalf("the mentor's own results hold Ana's: %+v", mine.Totals)
	}
	e.must("mentor", http.MethodGet, wsPath(ws, "/results/group"+whole), nil, &group, http.StatusOK)
	if group.Totals.Orders == 0 {
		t.Fatalf("the mentor does not see Ana's consented sum: %+v", group)
	}
	var s resSyncJSON
	e.must("bia", http.MethodGet, "/v1/me/results/sync", nil, &s, http.StatusOK)
	if s.Status != "never" {
		t.Fatalf("Bia sees a sync: %+v", s)
	}
}

// TestResultsLeakRLS checks the second barrier: even a query without filters
// only sees what the policy lets through.
func TestResultsLeakRLS(t *testing.T) {
	e := newResEnv(t)
	ws := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", ws)
	other := e.mentorship("intruder", "Outra").ID
	e.join("intruder", "bia", other)
	e.connect("ana", resAppIDAna, resSecretAna)
	e.save("ana", ws, 0)
	e.generateLinks()
	e.sync("ana")

	ctx := context.Background()
	count := func(sub string, workspace uuid.UUID, sql string) int {
		t.Helper()
		s := database.Scope{UserID: e.me(sub).ID.String()}
		if workspace != uuid.Nil {
			s.WorkspaceID = workspace.String()
		}
		var n int
		err := database.InTx(ctx, e.pool, s, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, sql).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	const all = "SELECT count(*) FROM conversions"
	total, inWS := count("ana", uuid.Nil, all), count("ana", ws, all)
	if total == 0 || inWS == 0 || inWS > total {
		t.Fatalf("Ana sees %d in total and %d in the mentorship", total, inWS)
	}
	if n := count("mentor", ws, all); n != 0 {
		t.Fatalf("the mentor sees %d conversions without consent", n)
	}
	e.must("ana", http.MethodPut, wsPath(ws, "/results/consent"), map[string]any{"shares_results": true}, nil, http.StatusOK)
	if n := count("mentor", ws, all); n != inWS {
		t.Fatalf("the mentor sees %d, Ana %d", n, inWS)
	}
	// Another workspace and another user see nothing, even with consent.
	for _, c := range []struct {
		sub string
		ws  uuid.UUID
	}{{"intruder", other}, {"intruder", ws}, {"bia", uuid.Nil}, {"bia", ws}, {"mentor", uuid.Nil}} {
		if n := count(c.sub, c.ws, all); n != 0 {
			t.Fatalf("%s in %v sees %d", c.sub, c.ws, n)
		}
	}
	// Each user sees only their own sync state.
	if n := count("ana", uuid.Nil, "SELECT count(*) FROM conversion_syncs"); n != 1 {
		t.Fatalf("Ana sees %d syncs", n)
	}
	if n := count("bia", uuid.Nil, "SELECT count(*) FROM conversion_syncs"); n != 0 {
		t.Fatalf("Bia sees %d syncs", n)
	}
	// Only the sync (no workspace) writes: inside a workspace, not even Ana
	// changes her conversions.
	err := database.InTx(ctx, e.pool, database.Scope{UserID: e.me("ana").ID.String(), WorkspaceID: ws.String()},
		func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE conversions SET commission_cents = 0")
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	var zeroed int
	if err := e.pool.QueryRow(ctx, "SELECT count(*) FROM conversions WHERE commission_cents = 0 AND status <> 'cancelled'").Scan(&zeroed); err != nil {
		t.Fatal(err)
	}
	if zeroed != 0 {
		t.Fatalf("the API changed %d conversions", zeroed)
	}
}

func TestResultsRequestSync(t *testing.T) {
	e := newResEnv(t)
	e.mustFail("ana", http.MethodPost, "/v1/me/results/sync", nil, http.StatusConflict, "sync_no_credential")
	e.connect("ana", resAppIDAna, resSecretAna)

	var s resSyncJSON
	e.must("ana", http.MethodGet, "/v1/me/results/sync", nil, &s, http.StatusOK)
	if s.Status != "never" || s.RequestedAt != nil {
		t.Fatalf("before: %+v", s)
	}
	e.must("ana", http.MethodPost, "/v1/me/results/sync", nil, &s, http.StatusAccepted)
	if s.Status != "syncing" || s.RequestedAt == nil || len(e.syncs.take()) != 1 {
		t.Fatalf("request: %+v", s)
	}
	// Asking again while it runs does not enqueue another.
	e.must("ana", http.MethodPost, "/v1/me/results/sync", nil, &s, http.StatusAccepted)
	if s.Status != "syncing" || len(e.syncs.take()) != 0 {
		t.Fatal("enqueued again")
	}
	e.sync("ana")
	e.must("ana", http.MethodGet, "/v1/me/results/sync", nil, &s, http.StatusOK)
	if s.Status != "ok" || s.Conversions == 0 || s.FinishedAt == nil || s.Error != nil {
		t.Fatalf("after: %+v", s)
	}
	e.mustFail("ana", http.MethodPost, "/v1/me/results/sync", nil, http.StatusTooManyRequests, "synced_recently")

	// Refused credential (the Secret changed at Shopee): the sync records the
	// error, without details of the call, and the connection turns invalid.
	e.connect("bia", resAppIDBia, resSecretBia)
	e.mock.Secrets[resAppIDBia] = "changed"
	e.sync("bia")
	e.must("bia", http.MethodGet, "/v1/me/results/sync", nil, &s, http.StatusOK)
	if s.Status != "error" || s.ErrorCode == nil || *s.ErrorCode != "sync_credential_refused" || s.Error == nil ||
		strings.Contains(*s.Error, resAppIDBia) || strings.Contains(*s.Error, resSecretBia) {
		t.Fatalf("refused credential: %+v", s)
	}
	var c shopeeConnectionJSON
	e.must("bia", http.MethodGet, "/v1/me/shopee", nil, &c, http.StatusOK)
	if c.Status != "invalid" {
		t.Fatalf("connection: %+v", c)
	}
	e.mustFail("bia", http.MethodPost, "/v1/me/results/sync", nil, http.StatusConflict, "sync_no_credential")

	// Without a credential at all, the job records no_credential.
	e.sync("cris")
	e.must("cris", http.MethodGet, "/v1/me/results/sync", nil, &s, http.StatusOK)
	if s.Status != "no_credential" {
		t.Fatalf("without credential: %+v", s)
	}
}
