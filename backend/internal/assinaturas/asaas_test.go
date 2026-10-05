package assinaturas_test

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

	"github.com/rafixcs/app-parceiros/backend/internal/assinaturas"
)

// servidorAsaas responde às chamadas que o gateway faz e guarda os corpos
// recebidos.
func servidorAsaas(t *testing.T, recebido map[string]json.RawMessage) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	ler := func(r *http.Request, chave string) {
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		recebido[chave] = json.RawMessage(b.Bytes())
	}
	mux.HandleFunc("POST /v3/customers", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("access_token") != "chave-secreta" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ler(r, "customers")
		_, _ = w.Write([]byte(`{"id":"cus_1"}`))
	})
	mux.HandleFunc("POST /v3/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		ler(r, "subscriptions")
		_, _ = w.Write([]byte(`{"id":"sub_1","nextDueDate":"2026-10-08"}`))
	})
	mux.HandleFunc("GET /v3/subscriptions/sub_1/payments", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"status":"PENDING","dueDate":"2026-10-09","invoiceUrl":"https://asaas.test/i/1"}]}`))
	})
	mux.HandleFunc("POST /v3/subscriptions/sub_1", func(w http.ResponseWriter, r *http.Request) {
		ler(r, "mudar")
		_, _ = w.Write([]byte(`{"id":"sub_1"}`))
	})
	mux.HandleFunc("DELETE /v3/subscriptions/sub_1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deleted":true}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestAsaasCriar(t *testing.T) {
	recebido := map[string]json.RawMessage{}
	s := servidorAsaas(t, recebido)
	gw := assinaturas.Asaas{URL: s.URL + "/v3", Chave: "chave-secreta"}

	ws := uuid.New()
	ext, err := gw.Criar(context.Background(), assinaturas.NovaAssinatura{
		WorkspaceID: ws, Descricao: "Mentoria", ValorCentavos: 14900,
		Vencimento: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Nome:       "Rafael", Email: "r@exemplo.br", CPFCNPJ: "39053344705",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ext.ClienteID != "cus_1" || ext.ID != "sub_1" || ext.URLPagamento != "https://asaas.test/i/1" {
		t.Fatalf("assinatura externa: %+v", ext)
	}
	// O vencimento da cobrança em aberto manda no próximo ciclo.
	if ext.ProximoCiclo.Format(time.DateOnly) != "2026-10-09" {
		t.Fatalf("próximo ciclo = %s", ext.ProximoCiclo)
	}
	var pedido struct {
		Valor       json.Number `json:"value"`
		NextDueDate string      `json:"nextDueDate"`
		Cycle       string      `json:"cycle"`
		BillingType string      `json:"billingType"`
		Referencia  string      `json:"externalReference"`
	}
	if err := json.Unmarshal(recebido["subscriptions"], &pedido); err != nil {
		t.Fatal(err)
	}
	// Dinheiro em centavos não passa por float em nenhum ponto.
	if pedido.Valor.String() != "149.00" || pedido.NextDueDate != "2026-10-08" {
		t.Fatalf("valor %q, vencimento %q", pedido.Valor, pedido.NextDueDate)
	}
	if pedido.Cycle != "MONTHLY" || pedido.BillingType != "UNDEFINED" || pedido.Referencia != ws.String() {
		t.Fatalf("pedido: %+v", pedido)
	}

	if err := gw.MudarValor(context.Background(), "sub_1", 2990); err != nil {
		t.Fatal(err)
	}
	var mudanca struct {
		Valor     json.Number `json:"value"`
		Pendentes bool        `json:"updatePendingPayments"`
	}
	if err := json.Unmarshal(recebido["mudar"], &mudanca); err != nil {
		t.Fatal(err)
	}
	if mudanca.Valor.String() != "29.90" || !mudanca.Pendentes {
		t.Fatalf("mudança: %+v", mudanca)
	}
	if err := gw.Cancelar(context.Background(), "sub_1"); err != nil {
		t.Fatal(err)
	}
}

func TestAsaasErroNaoVazaSegredo(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"invalid_cpfCnpj","description":"CPF ou CNPJ inválido."}]}`))
	}))
	defer s.Close()
	gw := assinaturas.Asaas{URL: s.URL, Chave: "chave-secreta", SegredoWebhook: "segredo-webhook"}
	_, err := gw.Criar(context.Background(), assinaturas.NovaAssinatura{CPFCNPJ: "1"})
	if err == nil {
		t.Fatal("quer erro")
	}
	msg := err.Error()
	if !bytes.Contains([]byte(msg), []byte("invalid_cpfCnpj")) {
		t.Fatalf("erro sem a descrição do gateway: %s", msg)
	}
	for _, segredo := range []string{"chave-secreta", "segredo-webhook"} {
		if bytes.Contains([]byte(msg), []byte(segredo)) {
			t.Fatalf("erro vazou segredo: %s", msg)
		}
	}
}

func TestAsaasEvento(t *testing.T) {
	gw := assinaturas.Asaas{Chave: "k", SegredoWebhook: "segredo"}
	aviso := func(segredo, corpo string) (assinaturas.Evento, error) {
		r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/cobranca", bytes.NewReader([]byte(corpo)))
		if segredo != "" {
			r.Header.Set("asaas-access-token", segredo)
		}
		return gw.Evento(r)
	}
	pago := `{"id":"evt_1","event":"PAYMENT_RECEIVED","payment":{"id":"pay_1","subscription":"sub_1","dueDate":"2026-10-08","invoiceUrl":"https://asaas.test/i/1"}}`

	if _, err := aviso("", pago); !errors.Is(err, assinaturas.ErrWebhookInvalido) {
		t.Fatalf("sem segredo: %v", err)
	}
	if _, err := aviso("errado", pago); !errors.Is(err, assinaturas.ErrWebhookInvalido) {
		t.Fatalf("segredo errado: %v", err)
	}
	e, err := aviso("segredo", pago)
	if err != nil {
		t.Fatal(err)
	}
	if e.Tipo != assinaturas.EventoPago || e.ID != "evt_1" || e.AssinaturaExterna != "sub_1" {
		t.Fatalf("evento: %+v", e)
	}
	if e.Vencimento.Format(time.DateOnly) != "2026-10-08" || e.URLPagamento != "https://asaas.test/i/1" {
		t.Fatalf("evento: %+v", e)
	}

	// A assinatura encerrada no gateway vem no outro formato.
	e, err = aviso("segredo", `{"id":"evt_2","event":"SUBSCRIPTION_DELETED","subscription":{"id":"sub_1"}}`)
	if err != nil || e.Tipo != assinaturas.EventoCancelado || e.AssinaturaExterna != "sub_1" {
		t.Fatalf("cancelamento: %+v, %v", e, err)
	}

	if _, err := aviso("segredo", `{"id":"evt_3","event":"PAYMENT_ANTICIPATED","payment":{"subscription":"sub_1"}}`); !errors.Is(err, assinaturas.ErrEventoIgnorado) {
		t.Fatalf("evento sem interesse: %v", err)
	}
	if _, err := aviso("segredo", `{"id":"evt_4","event":"PAYMENT_RECEIVED"}`); !errors.Is(err, assinaturas.ErrWebhookInvalido) {
		t.Fatalf("aviso sem assinatura: %v", err)
	}
	if _, err := aviso("segredo", `nao e json`); !errors.Is(err, assinaturas.ErrWebhookInvalido) {
		t.Fatalf("corpo inválido: %v", err)
	}
}

func TestCPFCNPJ(t *testing.T) {
	casos := map[string]string{
		"390.533.447-05":     "39053344705",
		"39053344705":        "39053344705",
		"11.222.333/0001-81": "11222333000181",
		" 11222333000181 ":   "11222333000181",
	}
	for entrada, quer := range casos {
		got, err := assinaturas.CPFCNPJ(entrada)
		if err != nil || got != quer {
			t.Fatalf("CPFCNPJ(%q) = %q, %v", entrada, got, err)
		}
	}
	for _, invalido := range []string{"", "123", "3905334470", "390533447050"} {
		if _, err := assinaturas.CPFCNPJ(invalido); err == nil {
			t.Fatalf("CPFCNPJ(%q) devia falhar", invalido)
		}
	}
}
