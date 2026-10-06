package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/billing"
)

// asaasServer answers the calls the gateway makes and keeps the bodies it
// received.
func asaasServer(t *testing.T, received map[string]json.RawMessage) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	read := func(r *http.Request, key string) {
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		received[key] = json.RawMessage(b.Bytes())
	}
	mux.HandleFunc("POST /v3/customers", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("access_token") != "secret-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		read(r, "customers")
		_, _ = w.Write([]byte(`{"id":"cus_1"}`))
	})
	mux.HandleFunc("POST /v3/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		read(r, "subscriptions")
		_, _ = w.Write([]byte(`{"id":"sub_1","nextDueDate":"2026-10-08"}`))
	})
	mux.HandleFunc("GET /v3/subscriptions/sub_1/payments", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"status":"PENDING","dueDate":"2026-10-09","invoiceUrl":"https://asaas.test/i/1"}]}`))
	})
	mux.HandleFunc("POST /v3/subscriptions/sub_1", func(w http.ResponseWriter, r *http.Request) {
		read(r, "change")
		_, _ = w.Write([]byte(`{"id":"sub_1"}`))
	})
	mux.HandleFunc("DELETE /v3/subscriptions/sub_1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deleted":true}`))
	})
	mux.HandleFunc("DELETE /v3/subscriptions/sub_gone", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestAsaasCreate(t *testing.T) {
	received := map[string]json.RawMessage{}
	s := asaasServer(t, received)
	gw := billing.Asaas{URL: s.URL + "/v3", APIKey: "secret-key"}

	ws := uuid.New()
	ext, err := gw.Create(context.Background(), domain.NewExternalSubscription{
		WorkspaceID: ws, Description: "Mentoria", AmountCents: 14900,
		DueDate: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Name:    "Rafael", Email: "r@example.com", TaxID: "39053344705",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ext.CustomerID != "cus_1" || ext.ID != "sub_1" || ext.PaymentURL != "https://asaas.test/i/1" {
		t.Fatalf("external subscription: %+v", ext)
	}
	// The due date of the open charge rules the next cycle.
	if ext.NextDueDate.Format(time.DateOnly) != "2026-10-09" {
		t.Fatalf("next due date = %s", ext.NextDueDate)
	}
	var req struct {
		Value       json.Number `json:"value"`
		NextDueDate string      `json:"nextDueDate"`
		Cycle       string      `json:"cycle"`
		BillingType string      `json:"billingType"`
		Reference   string      `json:"externalReference"`
	}
	if err := json.Unmarshal(received["subscriptions"], &req); err != nil {
		t.Fatal(err)
	}
	// Money in cents never goes through float.
	if req.Value.String() != "149.00" || req.NextDueDate != "2026-10-08" {
		t.Fatalf("value %q, due date %q", req.Value, req.NextDueDate)
	}
	if req.Cycle != "MONTHLY" || req.BillingType != "UNDEFINED" || req.Reference != ws.String() {
		t.Fatalf("request: %+v", req)
	}

	if err := gw.ChangeAmount(context.Background(), "sub_1", 2990); err != nil {
		t.Fatal(err)
	}
	var change struct {
		Value   json.Number `json:"value"`
		Pending bool        `json:"updatePendingPayments"`
	}
	if err := json.Unmarshal(received["change"], &change); err != nil {
		t.Fatal(err)
	}
	if change.Value.String() != "29.90" || !change.Pending {
		t.Fatalf("change: %+v", change)
	}
	if err := gw.Cancel(context.Background(), "sub_1"); err != nil {
		t.Fatal(err)
	}
	// Cancelling what no longer exists is not an error.
	if err := gw.Cancel(context.Background(), "sub_gone"); err != nil {
		t.Fatal(err)
	}
}

func TestAsaasErrorDoesNotLeakSecrets(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"invalid_cpfCnpj","description":"CPF ou CNPJ inválido."}]}`))
	}))
	defer s.Close()
	gw := billing.Asaas{URL: s.URL, APIKey: "secret-key", WebhookSecret: "webhook-secret"}
	_, err := gw.Create(context.Background(), domain.NewExternalSubscription{TaxID: "1"})
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if !bytes.Contains([]byte(msg), []byte("invalid_cpfCnpj")) {
		t.Fatalf("error without the gateway description: %s", msg)
	}
	for _, secret := range []string{"secret-key", "webhook-secret"} {
		if bytes.Contains([]byte(msg), []byte(secret)) {
			t.Fatalf("error leaked a secret: %s", msg)
		}
	}
}

func TestAsaasParseEvent(t *testing.T) {
	gw := billing.Asaas{APIKey: "k", WebhookSecret: "secret"}
	event := func(secret, body string) (domain.BillingEvent, error) {
		h := http.Header{}
		if secret != "" {
			h.Set("asaas-access-token", secret)
		}
		return gw.ParseEvent(h, []byte(body))
	}
	paid := `{"id":"evt_1","event":"PAYMENT_RECEIVED","payment":{"id":"pay_1","subscription":"sub_1","dueDate":"2026-10-08","invoiceUrl":"https://asaas.test/i/1"}}`

	if _, err := event("", paid); !errors.Is(err, domain.ErrInvalidBillingWebhook) {
		t.Fatalf("without secret: %v", err)
	}
	if _, err := event("wrong", paid); !errors.Is(err, domain.ErrInvalidBillingWebhook) {
		t.Fatalf("wrong secret: %v", err)
	}
	e, err := event("secret", paid)
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != domain.BillingEventPaid || e.ID != "evt_1" || e.ExternalSubscriptionID != "sub_1" || e.Provider != "asaas" {
		t.Fatalf("event: %+v", e)
	}
	if e.DueDate.Format(time.DateOnly) != "2026-10-08" || e.PaymentURL != "https://asaas.test/i/1" {
		t.Fatalf("event: %+v", e)
	}

	// The subscription ended at the gateway comes in the other format.
	e, err = event("secret", `{"id":"evt_2","event":"SUBSCRIPTION_DELETED","subscription":{"id":"sub_1"}}`)
	if err != nil || e.Kind != domain.BillingEventCancelled || e.ExternalSubscriptionID != "sub_1" {
		t.Fatalf("cancellation: %+v, %v", e, err)
	}

	if _, err := event("secret", `{"id":"evt_3","event":"PAYMENT_ANTICIPATED","payment":{"subscription":"sub_1"}}`); !errors.Is(err, domain.ErrBillingEventIgnored) {
		t.Fatalf("event of no interest: %v", err)
	}
	if _, err := event("secret", `{"id":"evt_4","event":"PAYMENT_RECEIVED"}`); !errors.Is(err, domain.ErrInvalidBillingWebhook) {
		t.Fatalf("event without subscription: %v", err)
	}
	if _, err := event("secret", `not json`); !errors.Is(err, domain.ErrInvalidBillingWebhook) {
		t.Fatalf("invalid body: %v", err)
	}
}

func TestTaxID(t *testing.T) {
	cases := map[string]string{
		"390.533.447-05":     "39053344705",
		"39053344705":        "39053344705",
		"11.222.333/0001-81": "11222333000181",
		" 11222333000181 ":   "11222333000181",
	}
	for in, want := range cases {
		got, err := domain.TaxID(in)
		if err != nil || got != want {
			t.Fatalf("TaxID(%q) = %q, %v", in, got, err)
		}
	}
	for _, invalid := range []string{"", "123", "3905334470", "390533447050"} {
		if _, err := domain.TaxID(invalid); !errors.Is(err, domain.ErrInvalidTaxID) {
			t.Fatalf("TaxID(%q) should fail, got %v", invalid, err)
		}
	}
}
