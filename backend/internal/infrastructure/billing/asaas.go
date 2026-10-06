// Package billing holds the payment gateways (domain.PaymentGateway): Asaas
// in production and a mock for the local environment and the tests.
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// AsaasURL is the production API; the sandbox is https://api-sandbox.asaas.com/v3.
const AsaasURL = "https://api.asaas.com/v3"

// Asaas is the Asaas payment gateway: a monthly subscription with the invoice
// open in PIX, boleto or card, as the payer chooses.
//
// The API key goes in the access_token header and the webhook secret in
// asaas-access-token, configured in the Asaas panel. Neither shows up in logs
// nor in errors.
type Asaas struct {
	URL string
	// APIKey is ASAAS_API_KEY.
	APIKey string
	// WebhookSecret is the token Asaas sends in asaas-access-token. Empty
	// refuses every event: without it there is no way to know the event is
	// from Asaas.
	WebhookSecret string
	HTTP          *http.Client
}

var _ domain.PaymentGateway = Asaas{}

func (a Asaas) Name() string { return "asaas" }

func (a Asaas) Simulated() bool { return false }

func (a Asaas) baseURL() string {
	if a.URL == "" {
		return AsaasURL
	}
	return strings.TrimRight(a.URL, "/")
}

func (a Asaas) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// reais formats cents as the decimal number Asaas expects, without going
// through float.
type reais int64

func (c reais) MarshalJSON() ([]byte, error) {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return []byte(fmt.Sprintf("%s%d.%02d", sign, int64(c)/100, int64(c)%100)), nil
}

type asaasDate string

func dateOf(t time.Time) asaasDate { return asaasDate(t.Format(time.DateOnly)) }

func (d asaasDate) time() time.Time {
	t, err := time.Parse(time.DateOnly, string(d))
	if err != nil {
		return time.Time{}
	}
	return t
}

// errAsaasNotFound is a 404 from Asaas.
var errAsaasNotFound = errors.New("not found")

// call makes the request and decodes the answer into out. Errors do not carry
// the request body, which has the payer's data.
func (a Asaas) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("access_token", a.APIKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("asaas %s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("asaas %s %s: %w", method, path, errAsaasNotFound)
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("asaas %s %s: %s: %w", method, path, res.Status, asaasError(b))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("asaas %s %s: unexpected answer: %w", method, path, err)
	}
	return nil
}

// asaasError extracts the description of the errors from the answer body.
func asaasError(b []byte) error {
	var r struct {
		Errors []struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.Errors) == 0 {
		return errors.New("error without description")
	}
	msgs := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		msgs = append(msgs, e.Code+": "+e.Description)
	}
	return errors.New(strings.Join(msgs, "; "))
}

func (a Asaas) Create(ctx context.Context, n domain.NewExternalSubscription) (domain.ExternalSubscription, error) {
	var customer struct {
		ID string `json:"id"`
	}
	err := a.call(ctx, http.MethodPost, "/customers", map[string]any{
		"name":                 n.Name,
		"email":                n.Email,
		"cpfCnpj":              n.TaxID,
		"externalReference":    n.WorkspaceID.String(),
		"notificationDisabled": false,
	}, &customer)
	if err != nil {
		return domain.ExternalSubscription{}, err
	}

	var sub struct {
		ID          string    `json:"id"`
		NextDueDate asaasDate `json:"nextDueDate"`
	}
	err = a.call(ctx, http.MethodPost, "/subscriptions", map[string]any{
		"customer":          customer.ID,
		"billingType":       "UNDEFINED", // the payer picks PIX, boleto or card
		"cycle":             "MONTHLY",
		"value":             reais(n.AmountCents),
		"nextDueDate":       dateOf(n.DueDate),
		"description":       n.Description,
		"externalReference": n.WorkspaceID.String(),
	}, &sub)
	if err != nil {
		return domain.ExternalSubscription{}, err
	}

	e := domain.ExternalSubscription{CustomerID: customer.ID, ID: sub.ID, NextDueDate: sub.NextDueDate.time()}
	// The first charge is created right after, asynchronously: if it does not
	// exist yet, the URL arrives in the charge created event.
	var payments struct {
		Data []struct {
			Status     string    `json:"status"`
			DueDate    asaasDate `json:"dueDate"`
			InvoiceURL string    `json:"invoiceUrl"`
		} `json:"data"`
	}
	if err := a.call(ctx, http.MethodGet, "/subscriptions/"+sub.ID+"/payments", nil, &payments); err == nil {
		for _, p := range payments.Data {
			if p.Status == "PENDING" || p.Status == "OVERDUE" {
				e.PaymentURL = p.InvoiceURL
				if d := p.DueDate.time(); !d.IsZero() {
					e.NextDueDate = d
				}
				break
			}
		}
	}
	return e, nil
}

func (a Asaas) ChangeAmount(ctx context.Context, externalID string, amountCents int64) error {
	return a.call(ctx, http.MethodPost, "/subscriptions/"+externalID, map[string]any{
		"value": reais(amountCents),
		// Charges already open follow the new amount.
		"updatePendingPayments": true,
	}, nil)
}

func (a Asaas) Cancel(ctx context.Context, externalID string) error {
	err := a.call(ctx, http.MethodDelete, "/subscriptions/"+externalID, nil, nil)
	if errors.Is(err, errAsaasNotFound) {
		return nil
	}
	return err
}

// asaasEvents maps the Asaas events to the billing event kinds. The ones not
// here are ignored.
var asaasEvents = map[string]domain.BillingEventKind{
	"PAYMENT_CREATED":                      domain.BillingEventCharge,
	"PAYMENT_UPDATED":                      domain.BillingEventCharge,
	"PAYMENT_RECEIVED":                     domain.BillingEventPaid,
	"PAYMENT_CONFIRMED":                    domain.BillingEventPaid,
	"PAYMENT_OVERDUE":                      domain.BillingEventOverdue,
	"PAYMENT_REFUNDED":                     domain.BillingEventRefunded,
	"PAYMENT_CHARGEBACK_REQUESTED":         domain.BillingEventRefunded,
	"PAYMENT_AWAITING_CHARGEBACK_REVERSAL": domain.BillingEventRefunded,
	"SUBSCRIPTION_DELETED":                 domain.BillingEventCancelled,
}

func (a Asaas) ParseEvent(h domain.HeaderReader, body []byte) (domain.BillingEvent, error) {
	if a.WebhookSecret == "" || h.Get("asaas-access-token") != a.WebhookSecret {
		return domain.BillingEvent{}, domain.ErrInvalidBillingWebhook
	}
	var p struct {
		ID      string `json:"id"`
		Event   string `json:"event"`
		Payment struct {
			Subscription string    `json:"subscription"`
			DueDate      asaasDate `json:"dueDate"`
			InvoiceURL   string    `json:"invoiceUrl"`
		} `json:"payment"`
		Subscription struct {
			ID string `json:"id"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return domain.BillingEvent{}, domain.ErrInvalidBillingWebhook
	}
	kind, ok := asaasEvents[p.Event]
	if !ok {
		return domain.BillingEvent{}, domain.ErrBillingEventIgnored
	}
	e := domain.BillingEvent{
		ID:                     p.ID,
		Provider:               a.Name(),
		Kind:                   kind,
		ExternalSubscriptionID: p.Payment.Subscription,
		DueDate:                p.Payment.DueDate.time(),
		PaymentURL:             p.Payment.InvoiceURL,
	}
	if e.ExternalSubscriptionID == "" {
		e.ExternalSubscriptionID = p.Subscription.ID
	}
	if e.ID == "" || e.ExternalSubscriptionID == "" {
		return domain.BillingEvent{}, domain.ErrInvalidBillingWebhook
	}
	return e, nil
}
