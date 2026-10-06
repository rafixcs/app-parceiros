package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Billing rules. The access of each workspace is a date (Workspace.AccessUntil):
// each confirmed payment extends it and, past that date, the workspace
// suspends itself, without depending on a job or on a webhook that may get
// lost.
const (
	// TrialPeriod is the trial of a new workspace (the date lives in the
	// accounts; billing only shows it).
	TrialPeriod = 7 * 24 * time.Hour
	// GracePeriod is the access beyond the paid cycle, so the next payment
	// has time to arrive.
	GracePeriod = 3 * 24 * time.Hour
	// BillingCycle is the length of a paid cycle.
	BillingCycle = 30 * 24 * time.Hour
	// FirstDueIn is the deadline of the first charge of a checkout.
	FirstDueIn = 3 * 24 * time.Hour
)

// SubscriptionStatus of a subscription at the gateway. SubscriptionNone is
// only shown: the workspace never subscribed (it is in the trial or already
// suspended).
type SubscriptionStatus string

const (
	SubscriptionNone      SubscriptionStatus = "none"
	SubscriptionPending   SubscriptionStatus = "pending"
	SubscriptionActive    SubscriptionStatus = "active"
	SubscriptionOverdue   SubscriptionStatus = "overdue"
	SubscriptionCancelled SubscriptionStatus = "cancelled"
)

// Subscription is the current subscription of a workspace at the payment
// gateway. Whoever pays is the owner: the solo affiliate pays their plan and
// the mentor pays per seat, one per affiliate.
type Subscription struct {
	WorkspaceID        uuid.UUID
	Provider           string
	ExternalCustomerID string
	ExternalID         string
	Status             SubscriptionStatus
	Seats              int32
	AmountCents        int64
	// NextDueDate is the due date of the open charge (or of the next one).
	NextDueDate time.Time
	// PaymentURL is the open invoice at the gateway (PIX, boleto or card).
	PaymentURL  *string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	CancelledAt *time.Time
}

// SubscriptionView is what the subscription screen shows.
type SubscriptionView struct {
	Plan string
	// AccessStatus of the workspace: trial, active, free or suspended.
	AccessStatus AccessStatus
	// AccessUntil is how long the workspace can be used.
	AccessUntil time.Time
	Status      SubscriptionStatus
	// Provider of the gateway, empty without a subscription.
	Provider string
	// PriceCents is the monthly price of the plan: of the workspace in the solo
	// plan, of each seat in the mentorship.
	PriceCents int64
	// Seats bought (0 without a subscription); SeatsInUse are the affiliates
	// and pending invites; MaxSeats is the ceiling of the plan.
	Seats      int64
	SeatsInUse int64
	MaxSeats   int64
	// AmountCents is the monthly total of the subscription (0 without one).
	AmountCents int64
	// NextDueDate is the due date of the open (or next) charge.
	NextDueDate *time.Time
	// PaymentURL is the open invoice at the gateway.
	PaymentURL *string
	// Simulated says the gateway is the local mock, so the screen can offer
	// the button that simulates the payment.
	Simulated bool
}

// NewSubscription is the data of a subscription to store after the gateway
// created it.
type NewSubscription struct {
	Provider           string
	ExternalCustomerID string
	ExternalID         string
	Seats              int32
	AmountCents        int64
	NextDueDate        time.Time
	PaymentURL         *string
	CreatedBy          uuid.UUID
}

// BillingUpdate is the status of a subscription coming from the gateway. Nil
// fields keep the stored value.
type BillingUpdate struct {
	Status      SubscriptionStatus
	NextDueDate *time.Time
	PaymentURL  *string
}

// BillingRepository stores the subscriptions and the processed gateway
// events. Missing records return ErrNotFound.
type BillingRepository interface {
	// Subscription returns the subscription of the actor's workspace.
	Subscription(ctx context.Context, a Actor) (Subscription, error)
	// SubscriptionByExternalID finds a subscription by its id at the gateway
	// (the webhook has no user nor workspace).
	SubscriptionByExternalID(ctx context.Context, provider, externalID string) (Subscription, error)
	// SaveSubscription stores the workspace's subscription. A new one only
	// replaces a cancelled one; with one in progress it returns false.
	SaveSubscription(ctx context.Context, a Actor, s NewSubscription) (bool, error)
	// UpdateSubscriptionPlan changes seats and amount of the subscription in
	// progress.
	UpdateSubscriptionPlan(ctx context.Context, a Actor, seats int32, amountCents int64) error
	// CancelSubscription marks the subscription in progress as cancelled.
	CancelSubscription(ctx context.Context, a Actor) error
	// UpdateBilling applies the status coming from the gateway, in the scope
	// of the subscription's workspace without a user.
	UpdateBilling(ctx context.Context, s Subscription, u BillingUpdate) error
	// RecordEvent records a gateway event and says whether it is new (false
	// for a redelivery).
	RecordEvent(ctx context.Context, s Subscription, e BillingEvent) (bool, error)
}

// NewExternalSubscription is the subscription request sent to the gateway.
type NewExternalSubscription struct {
	WorkspaceID uuid.UUID
	Description string
	AmountCents int64
	// DueDate of the first charge.
	DueDate time.Time
	// Payer data (the workspace owner).
	Name  string
	Email string
	TaxID string
}

// ExternalSubscription is the subscription created at the gateway.
type ExternalSubscription struct {
	CustomerID string
	ID         string
	// PaymentURL is the open invoice (PIX, boleto or card). It may be empty
	// and arrive later, in the charge created event.
	PaymentURL string
	// NextDueDate is the due date of the open charge.
	NextDueDate time.Time
}

// BillingEventKind is what a gateway event means for the subscription.
type BillingEventKind string

const (
	// BillingEventCharge is a charge created or updated, still open.
	BillingEventCharge BillingEventKind = "charge"
	// BillingEventPaid is a confirmed payment: it extends the access to the
	// end of the cycle.
	BillingEventPaid BillingEventKind = "paid"
	// BillingEventOverdue is a charge past due without payment. The access
	// ends by the date (AccessUntil), not by this event.
	BillingEventOverdue BillingEventKind = "overdue"
	// BillingEventRefunded is a refund or chargeback: it suspends at once.
	BillingEventRefunded BillingEventKind = "refunded"
	// BillingEventCancelled is the subscription ended at the gateway.
	BillingEventCancelled BillingEventKind = "cancelled"
)

// Valid says whether the kind is one of the known ones.
func (k BillingEventKind) Valid() bool {
	switch k {
	case BillingEventCharge, BillingEventPaid, BillingEventOverdue, BillingEventRefunded, BillingEventCancelled:
		return true
	}
	return false
}

// BillingEvent is a gateway event, already normalized.
type BillingEvent struct {
	// ID of the event at the provider, to ignore redeliveries.
	ID       string
	Provider string
	Kind     BillingEventKind
	// ExternalSubscriptionID is the id of the subscription at the provider.
	ExternalSubscriptionID string
	// DueDate of the charge of the event (zero when the event has no date).
	DueDate time.Time
	// PaymentURL of the open invoice, when the event brings one.
	PaymentURL string
}

// HeaderReader reads the headers of a webhook request (http.Header).
type HeaderReader interface {
	Get(key string) string
}

// PaymentGateway is the billing provider. Everything the module needs from a
// gateway is here, so the provider (Asaas, Mercado Pago, Stripe) can change
// without touching the rest: the service only keeps the ids the gateway
// returns and reacts to the normalized events.
type PaymentGateway interface {
	// Name is the provider stored in subscriptions.provider (e.g. "asaas").
	Name() string
	// Simulated says whether this is the local mock, which charges nothing
	// and accepts simulated payments.
	Simulated() bool
	// Create opens the monthly subscription and returns the provider ids and
	// the first charge.
	Create(ctx context.Context, n NewExternalSubscription) (ExternalSubscription, error)
	// ChangeAmount adjusts the monthly amount (more or fewer seats).
	ChangeAmount(ctx context.Context, externalID string, amountCents int64) error
	// Cancel ends the subscription at the provider. Cancelling what no longer
	// exists is not an error.
	Cancel(ctx context.Context, externalID string) error
	// ParseEvent checks the authenticity of a webhook and normalizes it. It
	// returns ErrInvalidBillingWebhook for a request that did not come from the
	// provider (or came broken) and ErrBillingEventIgnored for events that do
	// not matter.
	ParseEvent(h HeaderReader, body []byte) (BillingEvent, error)
}

// TaxID returns only the digits of a CPF or CNPJ, or ErrInvalidTaxID when the
// length is neither. It does not check the verification digits: the gateway
// does.
func TaxID(v string) (string, error) {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) != 11 && len(d) != 14 {
		return "", ErrInvalidTaxID
	}
	return d, nil
}

// Billing errors.
var (
	ErrBillingOwnerOnly      = NewError(KindForbidden, "billing_owner_only")
	ErrBillingManagersOnly   = NewError(KindForbidden, "billing_managers_only")
	ErrAlreadySubscribed     = NewError(KindConflict, "already_subscribed")
	ErrNoSubscription        = NewError(KindNotFound, "no_subscription")
	ErrInvalidTaxID          = NewError(KindInvalid, "invalid_tax_id")
	ErrMinimumSeats          = NewError(KindInvalid, "minimum_seats")
	ErrNoPrice               = NewError(KindConflict, "no_price")
	ErrSimulationOnlyLocal   = NewError(KindNotFound, "simulation_unavailable")
	ErrBillingUnavailable    = NewError(KindUpstream, "billing_unavailable")
	ErrInvalidBillingWebhook = NewError(KindUnauthenticated, "invalid_billing_webhook")

	// ErrBillingEventIgnored is a gateway event that changes nothing in the
	// subscription. It never reaches the customer.
	ErrBillingEventIgnored = errors.New("billing event ignored")
)
