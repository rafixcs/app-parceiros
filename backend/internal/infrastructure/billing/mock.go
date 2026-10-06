package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Mock is an in-memory payment gateway for the local environment and the
// tests (BILLING_MODE=mock): nothing is charged and the payment is confirmed
// by the simulation route (or by an event sent by hand). Never use it outside
// dev.
type Mock struct {
	mu sync.Mutex
	// fail makes every gateway call fail, to test the error path.
	fail          error
	seq           int
	subscriptions map[string]domain.NewExternalSubscription
	cancelled     map[string]bool
	amounts       map[string]int64
}

var _ domain.PaymentGateway = (*Mock)(nil)

func NewMock() *Mock {
	return &Mock{
		subscriptions: map[string]domain.NewExternalSubscription{},
		cancelled:     map[string]bool{},
		amounts:       map[string]int64{},
	}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Simulated() bool { return true }

// Fail makes every call fail with err (nil makes them work again).
func (m *Mock) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

// Subscriptions returns the subscriptions created, by external id.
func (m *Mock) Subscriptions() map[string]domain.NewExternalSubscription {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]domain.NewExternalSubscription, len(m.subscriptions))
	for k, v := range m.subscriptions {
		out[k] = v
	}
	return out
}

// Amount returns the current monthly amount of a subscription.
func (m *Mock) Amount(externalID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.amounts[externalID]
}

// Cancelled says whether a subscription was cancelled.
func (m *Mock) Cancelled(externalID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancelled[externalID]
}

func (m *Mock) Create(_ context.Context, n domain.NewExternalSubscription) (domain.ExternalSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return domain.ExternalSubscription{}, m.fail
	}
	m.seq++
	id := fmt.Sprintf("mock_sub_%d", m.seq)
	m.subscriptions[id] = n
	m.amounts[id] = n.AmountCents
	return domain.ExternalSubscription{
		CustomerID:  fmt.Sprintf("mock_cus_%d", m.seq),
		ID:          id,
		NextDueDate: n.DueDate,
	}, nil
}

func (m *Mock) ChangeAmount(_ context.Context, externalID string, amountCents int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.amounts[externalID] = amountCents
	return nil
}

func (m *Mock) Cancel(_ context.Context, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.cancelled[externalID] = true
	return nil
}

// ParseEvent accepts the event in the module's own format, for the local
// environment to simulate a payment:
//
//	{"id":"evt-1","kind":"paid","subscription":"mock_sub_1","due_date":"2026-10-08"}
func (m *Mock) ParseEvent(_ domain.HeaderReader, body []byte) (domain.BillingEvent, error) {
	var p struct {
		ID           string `json:"id"`
		Kind         string `json:"kind"`
		Subscription string `json:"subscription"`
		DueDate      string `json:"due_date"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.ID == "" || p.Subscription == "" {
		return domain.BillingEvent{}, domain.ErrInvalidBillingWebhook
	}
	e := domain.BillingEvent{
		ID: p.ID, Provider: m.Name(), Kind: domain.BillingEventKind(p.Kind), ExternalSubscriptionID: p.Subscription,
	}
	if !e.Kind.Valid() {
		return domain.BillingEvent{}, domain.ErrBillingEventIgnored
	}
	if p.DueDate != "" {
		t, err := time.Parse(time.DateOnly, p.DueDate)
		if err != nil {
			return domain.BillingEvent{}, domain.ErrInvalidBillingWebhook
		}
		e.DueDate = t
	}
	return e, nil
}
