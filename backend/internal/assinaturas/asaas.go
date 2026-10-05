package assinaturas

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
)

// URLAsaas é a API de produção; o sandbox é https://api-sandbox.asaas.com/v3.
const URLAsaas = "https://api.asaas.com/v3"

// Asaas é o gateway de cobrança Asaas: assinatura mensal com a fatura aberta
// em PIX, boleto ou cartão, à escolha de quem paga.
//
// A chave da API vai no cabeçalho access_token e o segredo do webhook no
// asaas-access-token, configurado no painel. Nenhum dos dois aparece em log
// nem em erro.
type Asaas struct {
	URL string
	// Chave da API (ASAAS_API_KEY).
	Chave string
	// SegredoWebhook é o token que o Asaas manda em asaas-access-token. Vazio
	// recusa todos os avisos: sem ele não há como saber que o aviso é dele.
	SegredoWebhook string
	HTTP           *http.Client
}

func (a Asaas) Nome() string { return "asaas" }

func (a Asaas) url() string {
	if a.URL == "" {
		return URLAsaas
	}
	return strings.TrimRight(a.URL, "/")
}

func (a Asaas) cliente() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// reais formata centavos como o número decimal que o Asaas espera, sem passar
// por float.
type reais int64

func (c reais) MarshalJSON() ([]byte, error) {
	sinal := ""
	if c < 0 {
		sinal, c = "-", -c
	}
	return []byte(fmt.Sprintf("%s%d.%02d", sinal, int64(c)/100, int64(c)%100)), nil
}

type dataAsaas string

func data(t time.Time) dataAsaas { return dataAsaas(t.Format(time.DateOnly)) }

func (d dataAsaas) tempo() time.Time {
	t, err := time.Parse(time.DateOnly, string(d))
	if err != nil {
		return time.Time{}
	}
	return t
}

// chamar faz a chamada e decodifica a resposta em out. Os erros não levam o
// corpo da requisição, que tem dados de quem paga.
func (a Asaas) chamar(ctx context.Context, metodo, caminho string, corpo, out any) error {
	var body io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, a.url()+caminho, body)
	if err != nil {
		return err
	}
	req.Header.Set("access_token", a.Chave)
	req.Header.Set("Accept", "application/json")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.cliente().Do(req)
	if err != nil {
		return fmt.Errorf("asaas %s %s: %w", metodo, caminho, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("asaas %s %s: %s: %w", metodo, caminho, res.Status, erroAsaas(b))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("asaas %s %s: resposta inesperada: %w", metodo, caminho, err)
	}
	return nil
}

// erroAsaas extrai a descrição dos erros do corpo da resposta.
func erroAsaas(b []byte) error {
	var r struct {
		Errors []struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.Errors) == 0 {
		return errors.New("erro sem descrição")
	}
	msgs := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		msgs = append(msgs, e.Code+": "+e.Description)
	}
	return errors.New(strings.Join(msgs, "; "))
}

func (a Asaas) Criar(ctx context.Context, n NovaAssinatura) (Externa, error) {
	var cliente struct {
		ID string `json:"id"`
	}
	err := a.chamar(ctx, http.MethodPost, "/customers", map[string]any{
		"name":                 n.Nome,
		"email":                n.Email,
		"cpfCnpj":              n.CPFCNPJ,
		"externalReference":    n.WorkspaceID.String(),
		"notificationDisabled": false,
	}, &cliente)
	if err != nil {
		return Externa{}, err
	}

	var assinatura struct {
		ID          string    `json:"id"`
		NextDueDate dataAsaas `json:"nextDueDate"`
	}
	err = a.chamar(ctx, http.MethodPost, "/subscriptions", map[string]any{
		"customer":          cliente.ID,
		"billingType":       "UNDEFINED", // quem paga escolhe PIX, boleto ou cartão
		"cycle":             "MONTHLY",
		"value":             reais(n.ValorCentavos),
		"nextDueDate":       data(n.Vencimento),
		"description":       n.Descricao,
		"externalReference": n.WorkspaceID.String(),
	}, &assinatura)
	if err != nil {
		return Externa{}, err
	}

	e := Externa{ClienteID: cliente.ID, ID: assinatura.ID, ProximoCiclo: assinatura.NextDueDate.tempo()}
	// A primeira cobrança é criada em seguida, de forma assíncrona: se ainda
	// não existe, a URL chega no evento de cobrança criada.
	var cobrancas struct {
		Data []struct {
			Status     string    `json:"status"`
			DueDate    dataAsaas `json:"dueDate"`
			InvoiceURL string    `json:"invoiceUrl"`
		} `json:"data"`
	}
	if err := a.chamar(ctx, http.MethodGet, "/subscriptions/"+assinatura.ID+"/payments", nil, &cobrancas); err == nil {
		for _, c := range cobrancas.Data {
			if c.Status == "PENDING" || c.Status == "OVERDUE" {
				e.URLPagamento = c.InvoiceURL
				if d := c.DueDate.tempo(); !d.IsZero() {
					e.ProximoCiclo = d
				}
				break
			}
		}
	}
	return e, nil
}

func (a Asaas) MudarValor(ctx context.Context, externoID string, valorCentavos int64) error {
	return a.chamar(ctx, http.MethodPost, "/subscriptions/"+externoID, map[string]any{
		"value": reais(valorCentavos),
		// As cobranças já em aberto seguem o novo valor.
		"updatePendingPayments": true,
	}, nil)
}

func (a Asaas) Cancelar(ctx context.Context, externoID string) error {
	err := a.chamar(ctx, http.MethodDelete, "/subscriptions/"+externoID, nil, nil)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}

// eventosAsaas liga os avisos do Asaas aos tipos do módulo. Os que não estão
// aqui são ignorados.
var eventosAsaas = map[string]TipoEvento{
	"PAYMENT_CREATED":                      EventoCobranca,
	"PAYMENT_UPDATED":                      EventoCobranca,
	"PAYMENT_RECEIVED":                     EventoPago,
	"PAYMENT_CONFIRMED":                    EventoPago,
	"PAYMENT_OVERDUE":                      EventoAtrasado,
	"PAYMENT_REFUNDED":                     EventoEstornado,
	"PAYMENT_CHARGEBACK_REQUESTED":         EventoEstornado,
	"PAYMENT_AWAITING_CHARGEBACK_REVERSAL": EventoEstornado,
	"SUBSCRIPTION_DELETED":                 EventoCancelado,
}

func (a Asaas) Evento(r *http.Request) (Evento, error) {
	if a.SegredoWebhook == "" || r.Header.Get("asaas-access-token") != a.SegredoWebhook {
		return Evento{}, ErrWebhookInvalido
	}
	var p struct {
		ID      string `json:"id"`
		Event   string `json:"event"`
		Payment struct {
			Subscription string    `json:"subscription"`
			DueDate      dataAsaas `json:"dueDate"`
			InvoiceURL   string    `json:"invoiceUrl"`
		} `json:"payment"`
		Subscription struct {
			ID string `json:"id"`
		} `json:"subscription"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&p); err != nil {
		return Evento{}, ErrWebhookInvalido
	}
	tipo, ok := eventosAsaas[p.Event]
	if !ok {
		return Evento{}, ErrEventoIgnorado
	}
	e := Evento{
		ID:                p.ID,
		Provedor:          a.Nome(),
		Tipo:              tipo,
		AssinaturaExterna: p.Payment.Subscription,
		Vencimento:        p.Payment.DueDate.tempo(),
		URLPagamento:      p.Payment.InvoiceURL,
	}
	if e.AssinaturaExterna == "" {
		e.AssinaturaExterna = p.Subscription.ID
	}
	if e.ID == "" || e.AssinaturaExterna == "" {
		return Evento{}, ErrWebhookInvalido
	}
	return e, nil
}

// CPFCNPJ devolve só os dígitos de um CPF ou CNPJ, ou erro se o tamanho não
// for de nenhum dos dois. Não valida os dígitos verificadores: isso o gateway
// faz.
func CPFCNPJ(v string) (string, error) {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) != 11 && len(d) != 14 {
		return "", ErrCPFCNPJ
	}
	return d, nil
}
