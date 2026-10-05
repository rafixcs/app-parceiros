package assinaturas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Mock é um gateway de cobrança em memória, para o ambiente local e os testes:
// nada é cobrado e o pagamento é confirmado pela rota de simulação (ou por um
// evento montado à mão). Nunca use fora de dev.
type Mock struct {
	// Falhar faz todas as chamadas ao gateway falharem, para testar o caminho
	// de erro.
	Falhar error

	mu          sync.Mutex
	seq         int
	Assinaturas map[string]NovaAssinatura
	Canceladas  map[string]bool
	Valores     map[string]int64
}

func NovoMock() *Mock {
	return &Mock{
		Assinaturas: map[string]NovaAssinatura{},
		Canceladas:  map[string]bool{},
		Valores:     map[string]int64{},
	}
}

func (m *Mock) Nome() string { return "mock" }

func (m *Mock) Criar(_ context.Context, n NovaAssinatura) (Externa, error) {
	if m.Falhar != nil {
		return Externa{}, m.Falhar
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("mock_sub_%d", m.seq)
	m.Assinaturas[id] = n
	m.Valores[id] = n.ValorCentavos
	return Externa{
		ClienteID:    fmt.Sprintf("mock_cus_%d", m.seq),
		ID:           id,
		ProximoCiclo: n.Vencimento,
	}, nil
}

func (m *Mock) MudarValor(_ context.Context, externoID string, valorCentavos int64) error {
	if m.Falhar != nil {
		return m.Falhar
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Valores[externoID] = valorCentavos
	return nil
}

func (m *Mock) Cancelar(_ context.Context, externoID string) error {
	if m.Falhar != nil {
		return m.Falhar
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Canceladas[externoID] = true
	return nil
}

// Evento aceita o aviso no formato do próprio módulo, para o ambiente local
// simular um pagamento:
//
//	{"id":"evt-1","tipo":"pago","assinatura":"mock_sub_1","vencimento":"2026-10-08"}
func (m *Mock) Evento(r *http.Request) (Evento, error) {
	var p struct {
		ID         string `json:"id"`
		Tipo       string `json:"tipo"`
		Assinatura string `json:"assinatura"`
		Vencimento string `json:"vencimento"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&p); err != nil {
		return Evento{}, ErrWebhookInvalido
	}
	if p.ID == "" || p.Assinatura == "" {
		return Evento{}, ErrWebhookInvalido
	}
	e := Evento{ID: p.ID, Provedor: m.Nome(), Tipo: TipoEvento(p.Tipo), AssinaturaExterna: p.Assinatura}
	switch e.Tipo {
	case EventoCobranca, EventoPago, EventoAtrasado, EventoEstornado, EventoCancelado:
	default:
		return Evento{}, ErrEventoIgnorado
	}
	if p.Vencimento != "" {
		t, err := time.Parse(time.DateOnly, p.Vencimento)
		if err != nil {
			return Evento{}, ErrWebhookInvalido
		}
		e.Vencimento = t
	}
	return e, nil
}
