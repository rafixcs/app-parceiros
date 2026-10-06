package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/billing"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

// withPaymentGateway makes the app charge through gw.
func withPaymentGateway(gw domain.PaymentGateway) func(*infra) {
	return func(in *infra) { in.payments = gw }
}

type subscriptionJSON struct {
	Plan         string    `json:"plan"`
	AccessStatus string    `json:"access_status"`
	AccessUntil  time.Time `json:"access_until"`
	Status       string    `json:"status"`
	Provider     string    `json:"provider"`
	PriceCents   int64     `json:"price_cents"`
	Seats        int64     `json:"seats"`
	SeatsInUse   int64     `json:"seats_in_use"`
	MaxSeats     int64     `json:"max_seats"`
	AmountCents  int64     `json:"amount_cents"`
	NextDueDate  *string   `json:"next_due_date"`
	PaymentURL   *string   `json:"payment_url"`
	Simulated    bool      `json:"simulated"`
}

// billingApp is the test app with the mock gateway.
type billingApp struct {
	*testApp
	gw *billing.Mock
}

func newBillingApp(t *testing.T) *billingApp {
	t.Helper()
	gw := billing.NewMock()
	return &billingApp{testApp: newTestApp(t, withPaymentGateway(gw)), gw: gw}
}

// event sends a billing event through the webhook, as the gateway would.
func (a *billingApp) event(id, kind, externalID string, due time.Time, status int) {
	a.t.Helper()
	body := map[string]any{"id": id, "kind": kind, "subscription": externalID}
	if !due.IsZero() {
		body["due_date"] = due.Format(time.DateOnly)
	}
	a.must("", http.MethodPost, "/v1/webhooks/billing", body, nil, status)
}

// externalID returns the only subscription created at the gateway.
func (a *billingApp) externalID() string {
	a.t.Helper()
	for id := range a.gw.Subscriptions() {
		return id
	}
	a.t.Fatal("no subscription created at the gateway")
	return ""
}

func (a *billingApp) subscription(sub string, ws uuid.UUID) subscriptionJSON {
	a.t.Helper()
	var v subscriptionJSON
	a.must(sub, http.MethodGet, wsPath(ws, "/subscription"), nil, &v, http.StatusOK)
	return v
}

func (a *billingApp) member(sub string, ws uuid.UUID) domain.Member {
	a.t.Helper()
	m, err := a.svcs.accounts.Member(context.Background(), a.me(sub).ID, ws)
	if err != nil {
		a.t.Fatal(err)
	}
	return m
}

func TestBillingCheckoutAndPayment(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma de outubro").ID
	path := wsPath(ws, "/subscription")

	v := a.subscription("mentor", ws)
	if v.Status != "none" || v.AccessStatus != "trial" || v.PriceCents != 1490 {
		t.Fatalf("in the trial: %+v", v)
	}
	if v.MaxSeats != 200 || v.SeatsInUse != 0 || !v.Simulated {
		t.Fatalf("limits: %+v", v)
	}

	a.mustFail("mentor", http.MethodPost, path, map[string]any{"seats": 10, "tax_id": "123"},
		http.StatusUnprocessableEntity, "invalid_tax_id")
	a.mustFail("mentor", http.MethodPost, path, map[string]any{"seats": 0, "tax_id": "390.533.447-05"},
		http.StatusUnprocessableEntity, "minimum_seats")

	a.must("mentor", http.MethodPost, path, map[string]any{"seats": 10, "tax_id": "390.533.447-05"}, &v, http.StatusCreated)
	if v.Status != "pending" || v.AmountCents != 10*1490 || v.Seats != 10 || v.Provider != "mock" {
		t.Fatalf("after subscribing: %+v", v)
	}
	// Subscribing does not pay: the workspace stays in the trial.
	if v.AccessStatus != "trial" {
		t.Fatalf("access status = %q", v.AccessStatus)
	}
	external := a.externalID()
	if n := a.gw.Subscriptions()[external]; n.AmountCents != 10*1490 || n.TaxID != "39053344705" || n.Email == "" {
		t.Fatalf("request at the gateway: %+v", n)
	}
	a.mustFail("mentor", http.MethodPost, path, map[string]any{"seats": 10, "tax_id": "390.533.447-05"},
		http.StatusConflict, "already_subscribed")

	// The charge created brings the due date.
	due := time.Now().UTC().Add(72 * time.Hour).Truncate(24 * time.Hour)
	a.event("evt-charge", "charge", external, due, http.StatusNoContent)
	v = a.subscription("mentor", ws)
	if v.NextDueDate == nil || *v.NextDueDate != due.Format(time.DateOnly) {
		t.Fatalf("next due date = %v", v.NextDueDate)
	}

	// Confirmed payment: workspace active until the end of the cycle, with
	// the seats bought.
	a.event("evt-paid", "paid", external, due, http.StatusNoContent)
	v = a.subscription("mentor", ws)
	end := due.Add(domain.BillingCycle + domain.GracePeriod)
	if v.Status != "active" || v.AccessStatus != "active" || v.PaymentURL != nil {
		t.Fatalf("after the payment: %+v", v)
	}
	if v.AccessUntil.Sub(end).Abs() > time.Minute || v.Seats != 10 {
		t.Fatalf("access until %s (want %s), seats %d", v.AccessUntil, end, v.Seats)
	}
	if ws := a.personal("mentor"); ws.Seats != nil {
		t.Fatalf("the personal workspace got seats: %+v", ws)
	}

	// A redelivery of the same event changes nothing.
	before := v
	a.event("evt-paid", "paid", external, due.Add(30*24*time.Hour), http.StatusNoContent)
	v = a.subscription("mentor", ws)
	if v.AccessUntil.Sub(before.AccessUntil).Abs() > time.Second {
		t.Fatalf("redelivery changed the access: %s -> %s", before.AccessUntil, v.AccessUntil)
	}

	// Overdue does not cut the access at once (it ends by the date); a refund
	// does.
	a.event("evt-overdue", "overdue", external, due, http.StatusNoContent)
	v = a.subscription("mentor", ws)
	if v.Status != "overdue" || v.AccessStatus != "active" {
		t.Fatalf("overdue: %+v", v)
	}
	a.event("evt-refund", "refunded", external, time.Time{}, http.StatusNoContent)
	v = a.subscription("mentor", ws)
	if v.AccessStatus != "suspended" {
		t.Fatalf("after the refund: %+v", v)
	}
}

func TestBillingSoloPlan(t *testing.T) {
	a := newBillingApp(t)
	ws := a.personal("ana").ID
	path := wsPath(ws, "/subscription")

	v := a.subscription("ana", ws)
	if v.Plan != "solo" || v.PriceCents != 2990 || v.MaxSeats != 1 {
		t.Fatalf("solo plan: %+v", v)
	}
	// Seats asked for in the solo plan are ignored: it is a one-person
	// workspace.
	a.must("ana", http.MethodPost, path, map[string]any{"seats": 7, "tax_id": "39053344705"}, &v, http.StatusCreated)
	if v.AmountCents != 2990 || v.Seats != 1 {
		t.Fatalf("solo subscription: %+v", v)
	}
	// Simulating the payment (only with the mock gateway) activates the
	// workspace.
	a.must("ana", http.MethodPost, path+"/simulate-payment", nil, &v, http.StatusOK)
	if v.Status != "active" || v.AccessStatus != "active" {
		t.Fatalf("after simulating: %+v", v)
	}
}

func TestBillingSuspendedWorkspacePays(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	a.admin("UPDATE workspaces SET access_until = now() - interval '1 day' WHERE id = $1", ws)

	// With the workspace suspended, the subscription is still reachable: it
	// is how the workspace comes back.
	a.mustFail("mentor", http.MethodGet, wsPath(ws, "/members"), nil, http.StatusPaymentRequired, "workspace_suspended_owner")
	v := a.subscription("mentor", ws)
	if v.AccessStatus != "suspended" {
		t.Fatalf("access status = %q", v.AccessStatus)
	}
	a.must("mentor", http.MethodPost, wsPath(ws, "/subscription"), map[string]any{"seats": 1, "tax_id": "39053344705"}, &v, http.StatusCreated)
	a.event("evt-1", "paid", a.externalID(), time.Now().UTC().Truncate(24*time.Hour), http.StatusNoContent)
	a.must("mentor", http.MethodGet, wsPath(ws, "/members"), nil, &[]memberJSON{}, http.StatusOK)
}

func TestBillingChangeSeatsAndCancel(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	path := wsPath(ws, "/subscription")
	a.admin("UPDATE plan_limits SET value = 5 WHERE plan = 'mentorship' AND key = 'seats'")
	var v subscriptionJSON
	a.must("mentor", http.MethodPost, path, map[string]any{"seats": 4, "tax_id": "39053344705"}, &v, http.StatusCreated)
	external := a.externalID()
	a.event("evt-1", "paid", external, time.Now().UTC().Truncate(24*time.Hour), http.StatusNoContent)

	a.join("mentor", "bia", ws)
	a.join("mentor", "caio", ws)

	a.mustFail("mentor", http.MethodPatch, path, map[string]any{"seats": 9}, http.StatusUnprocessableEntity, "seats_above_plan")
	a.mustFail("mentor", http.MethodPatch, path, map[string]any{"seats": 1}, http.StatusConflict, "seats_in_use")

	// Fewer seats apply at once.
	a.must("mentor", http.MethodPatch, path, map[string]any{"seats": 2}, &v, http.StatusOK)
	if v.Seats != 2 || v.AmountCents != 2*1490 || a.gw.Amount(external) != 2*1490 {
		t.Fatalf("after reducing: %+v (gateway %d)", v, a.gw.Amount(external))
	}
	a.mustFail("mentor", http.MethodPost, wsPath(ws, "/invites"), map[string]any{}, http.StatusConflict, "no_seats")

	// More seats apply with the next payment.
	a.must("mentor", http.MethodPatch, path, map[string]any{"seats": 5}, &v, http.StatusOK)
	a.mustFail("mentor", http.MethodPost, wsPath(ws, "/invites"), map[string]any{}, http.StatusConflict, "no_seats")
	a.event("evt-2", "paid", external, time.Now().UTC().Truncate(24*time.Hour), http.StatusNoContent)
	a.invite("mentor", ws, nil)

	// Cancelling ends it at the gateway and keeps the access already paid.
	a.must("mentor", http.MethodDelete, path, nil, &v, http.StatusOK)
	if v.Status != "cancelled" || !a.gw.Cancelled(external) || v.AccessStatus != "active" {
		t.Fatalf("after cancelling: %+v", v)
	}
	a.mustFail("mentor", http.MethodDelete, path, nil, http.StatusNotFound, "no_subscription")
	a.mustFail("mentor", http.MethodPatch, path, map[string]any{"seats": 3}, http.StatusNotFound, "no_subscription")
	// After cancelling it can subscribe again.
	a.must("mentor", http.MethodPost, path, map[string]any{"seats": 3, "tax_id": "39053344705"}, &v, http.StatusCreated)
	if v.Status != "pending" || v.Seats != 3 {
		t.Fatalf("new subscription: %+v", v)
	}
}

func TestBillingPermissions(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	path := wsPath(ws, "/subscription")
	a.join("mentor", "bia", ws)
	a.must("mentor", http.MethodPost, path, map[string]any{"seats": 2, "tax_id": "39053344705"}, nil, http.StatusCreated)

	// The affiliate neither sees nor changes the subscription.
	a.mustFail("bia", http.MethodGet, path, nil, http.StatusForbidden, "billing_managers_only")
	a.mustFail("bia", http.MethodPost, path, map[string]any{"seats": 2, "tax_id": "39053344705"}, http.StatusForbidden, "billing_owner_only")
	a.mustFail("bia", http.MethodPatch, path, map[string]any{"seats": 3}, http.StatusForbidden, "billing_owner_only")
	a.mustFail("bia", http.MethodDelete, path, nil, http.StatusForbidden, "billing_owner_only")
	a.mustFail("bia", http.MethodPost, path+"/simulate-payment", nil, http.StatusForbidden, "billing_owner_only")

	// Whoever is outside the workspace does not even reach the module.
	a.mustFail("intruder", http.MethodGet, path, nil, http.StatusNotFound, "workspace_not_found")
	a.mustFail("intruder", http.MethodDelete, path, nil, http.StatusNotFound, "workspace_not_found")
	a.mustFail("", http.MethodGet, path, nil, http.StatusUnauthorized, "unauthenticated")
}

// TestBillingLeakBetweenWorkspaces checks that RLS hides the subscription of
// a workspace from whoever does not manage it, even when the service is called
// with a forged member.
func TestBillingLeakBetweenWorkspaces(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	a.must("mentor", http.MethodPost, wsPath(ws, "/subscription"), map[string]any{"seats": 3, "tax_id": "39053344705"}, nil, http.StatusCreated)
	other := a.mentorship("other", "Outra").ID

	ctx := context.Background()
	intruder := a.me("intruder")
	forged := domain.Member{
		WorkspaceID: ws, UserID: intruder.ID, Role: domain.RoleOwner, WorkspaceKind: domain.WorkspaceMentorship,
	}
	view, err := a.svcs.billing.View(ctx, forged)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.SubscriptionNone || view.AmountCents != 0 || view.PaymentURL != nil {
		t.Fatalf("subscription of another workspace showed up: %+v", view)
	}
	if _, err := a.svcs.billing.Cancel(ctx, forged); !errors.Is(err, domain.ErrNoSubscription) {
		t.Fatalf("cancelling from outside: %v", err)
	}
	// The owner of another workspace, in their own workspace, sees nothing
	// of this one either.
	if v := a.subscription("other", other); v.Status != "none" {
		t.Fatalf("other workspace: %+v", v)
	}

	// Straight at the database: another workspace's scope reads no row, and
	// the webhook scope of another subscription neither.
	repo := repository.NewPostgresBilling(a.pool)
	if _, err := repo.Subscription(ctx, domain.Actor{UserID: a.me("other").ID, WorkspaceID: other}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("subscription read from another workspace: %v", err)
	}
	var n int
	err = database.InTx(ctx, a.pool, database.Scope{ExternalSubscriptionID: "mock_sub_999"}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM subscriptions").Scan(&n)
	})
	if err != nil || n != 0 {
		t.Fatalf("subscriptions seen with another external id: %d, %v", n, err)
	}
	err = database.InTx(ctx, a.pool, database.Scope{WorkspaceID: other.String()}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM billing_events").Scan(&n)
	})
	if err != nil || n != 0 {
		t.Fatalf("billing events seen from another workspace: %d, %v", n, err)
	}

	// The subscription is still there, untouched.
	v := a.subscription("mentor", ws)
	if v.Status != "pending" || v.Seats != 3 {
		t.Fatalf("subscription changed from outside: %+v", v)
	}
}

func TestBillingWebhookUnknownAndInvalid(t *testing.T) {
	a := newBillingApp(t)
	// A subscription that is not ours: nothing to do, and the gateway need
	// not redeliver.
	a.event("evt-x", "paid", "mock_sub_999", time.Now(), http.StatusNoContent)
	// An event without subscription, or of a kind that does not matter.
	a.mustFail("", http.MethodPost, "/v1/webhooks/billing", map[string]any{"id": "evt-y", "kind": "paid"},
		http.StatusUnauthorized, "invalid_billing_webhook")
	a.event("evt-z", "something_else", "mock_sub_1", time.Time{}, http.StatusNoContent)
}

func TestBillingGatewayFailure(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	path := wsPath(ws, "/subscription")
	a.gw.Fail(errors.New("gateway down"))
	a.mustFail("mentor", http.MethodPost, path, map[string]any{"seats": 2, "tax_id": "39053344705"},
		http.StatusBadGateway, "billing_unavailable")
	// Nothing was stored: the checkout can be repeated.
	a.gw.Fail(nil)
	var v subscriptionJSON
	a.must("mentor", http.MethodPost, path, map[string]any{"seats": 2, "tax_id": "39053344705"}, &v, http.StatusCreated)
	if v.Status != "pending" {
		t.Fatalf("%+v", v)
	}
}

func TestBillingNoSimulationOutsideMock(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewBillingService(repository.NewPostgresBilling(a.pool), database.Transactor{Pool: a.pool},
		billing.Asaas{APIKey: "x"}, a.svcs.accounts, nil, log)
	if _, err := svc.SimulatePayment(context.Background(), a.member("mentor", ws)); !errors.Is(err, domain.ErrSimulationOnlyLocal) {
		t.Fatalf("simulation with a real gateway: %v", err)
	}
	if v, err := svc.View(context.Background(), a.member("mentor", ws)); err != nil || v.Simulated {
		t.Fatalf("view with a real gateway: %+v, %v", v, err)
	}
}

// fakeNotifier records the notices.
type fakeNotifier struct{ notices []domain.Notice }

func (f *fakeNotifier) Notify(_ context.Context, n domain.Notice) error {
	f.notices = append(f.notices, n)
	return nil
}

func TestBillingNotifiesTheOwner(t *testing.T) {
	a := newBillingApp(t)
	ws := a.mentorship("mentor", "Turma").ID
	notifier := &fakeNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewBillingService(repository.NewPostgresBilling(a.pool), database.Transactor{Pool: a.pool},
		a.gw, a.svcs.accounts, notifier, log)
	m := a.member("mentor", ws)
	if _, err := svc.Subscribe(context.Background(), m, service.NewSubscriptionRequest{Seats: 1, TaxID: "39053344705"}); err != nil {
		t.Fatal(err)
	}
	ext := a.externalID()
	for _, e := range []domain.BillingEvent{
		{ID: "e1", Kind: domain.BillingEventCharge},
		{ID: "e2", Kind: domain.BillingEventPaid},
		{ID: "e2", Kind: domain.BillingEventPaid}, // redelivery
		{ID: "e3", Kind: domain.BillingEventOverdue},
	} {
		e.Provider, e.ExternalSubscriptionID = "mock", ext
		if err := svc.Process(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if len(notifier.notices) != 2 {
		t.Fatalf("notices: %+v", notifier.notices)
	}
	n := notifier.notices[0]
	if n.Kind != "billing" || n.Key != "billing:e2" || n.Title != "Pagamento confirmado" ||
		len(n.UserIDs) != 1 || n.UserIDs[0] != m.UserID || n.WorkspaceID != ws {
		t.Fatalf("notice: %+v", n)
	}
}
