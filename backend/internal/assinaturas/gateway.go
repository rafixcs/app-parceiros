package assinaturas

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Gateway é o provedor de cobrança. Tudo o que o módulo precisa de um gateway
// está aqui, para trocar de provedor (Asaas, Mercado Pago, Stripe) sem mexer
// no resto: o serviço só guarda os ids que o gateway devolve e reage aos
// eventos normalizados por Evento.
type Gateway interface {
	// Nome é o provedor gravado em assinaturas.provedor (ex.: "asaas").
	Nome() string
	// Criar abre a assinatura mensal e devolve os ids do provedor e a primeira
	// cobrança.
	Criar(ctx context.Context, n NovaAssinatura) (Externa, error)
	// MudarValor ajusta o valor mensal (mais ou menos assentos). Vale do
	// próximo ciclo em diante.
	MudarValor(ctx context.Context, externoID string, valorCentavos int64) error
	// Cancelar encerra a assinatura no provedor. Cancelar o que já não existe
	// não é erro.
	Cancelar(ctx context.Context, externoID string) error
	// Evento interpreta um webhook: confere a autenticidade e normaliza o
	// aviso. ErrEventoIgnorado descarta avisos que não interessam.
	Evento(r *http.Request) (Evento, error)
}

// NovaAssinatura é o pedido de assinatura enviado ao gateway.
type NovaAssinatura struct {
	WorkspaceID   uuid.UUID
	Descricao     string
	ValorCentavos int64
	// Primeiro vencimento.
	Vencimento time.Time
	// Dados de quem paga (o dono do workspace).
	Nome    string
	Email   string
	CPFCNPJ string
}

// Externa é a assinatura criada no gateway.
type Externa struct {
	ClienteID string
	ID        string
	// URLPagamento é a fatura em aberto (PIX, boleto ou cartão). Pode vir
	// vazia e chegar depois, no evento de cobrança criada.
	URLPagamento string
	// ProximoCiclo é o vencimento da cobrança em aberto.
	ProximoCiclo time.Time
}

// TipoEvento é o que um aviso do gateway quer dizer para a assinatura.
type TipoEvento string

const (
	// EventoCobranca é uma cobrança criada ou atualizada, ainda em aberto.
	EventoCobranca TipoEvento = "cobranca"
	// EventoPago é um pagamento confirmado: libera o acesso até o fim do ciclo.
	EventoPago TipoEvento = "pago"
	// EventoAtrasado é uma cobrança vencida sem pagamento. O acesso cai pela
	// data (acesso_ate), não por este aviso.
	EventoAtrasado TipoEvento = "atrasado"
	// EventoEstornado é um estorno ou contestação: suspende na hora.
	EventoEstornado TipoEvento = "estornado"
	// EventoCancelado é a assinatura encerrada no gateway.
	EventoCancelado TipoEvento = "cancelado"
)

// Evento é o aviso do gateway, já normalizado.
type Evento struct {
	// ID do evento no provedor, para ignorar reenvios.
	ID string
	// Provedor que mandou o aviso.
	Provedor string
	Tipo     TipoEvento
	// AssinaturaExterna é o id da assinatura no provedor.
	AssinaturaExterna string
	// Vencimento da cobrança do aviso (zero quando o aviso não tem data).
	Vencimento time.Time
	// URLPagamento da fatura em aberto, quando o aviso traz uma.
	URLPagamento string
}
