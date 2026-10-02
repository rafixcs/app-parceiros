// Package fontes define o formato comum das ofertas vindas dos marketplaces.
// O MVP tem só a Shopee (fontes/shopee); TikTok Shop e outros entram atrás
// da mesma interface.
package fontes

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type Fonte string

const Shopee Fonte = "shopee"

// Oferta é um produto como a fonte o informa num instante. Dinheiro em
// centavos e comissão em basis points (1% = 100).
type Oferta struct {
	ItemID           int64
	LojaID           int64
	LojaNome         string
	Nome             string
	ImagemURL        string
	URL              string  // página do produto, sem link de afiliado
	Categorias       []int64 // da mais geral para a mais específica
	PrecoMinCentavos int64
	PrecoMaxCentavos int64
	ComissaoBP       int32
	Vendas           int64
	Nota             *float64 // nil quando o produto ainda não tem avaliações
}

type FiltroCatalogo struct {
	CategoriaID int64 // 0 = todas
	Pagina      int   // começa em 1
	Limite      int
}

type PaginaCatalogo struct {
	Ofertas    []Oferta
	TemProxima bool
	// Bruto é a resposta original, guardada para reprocessar se o parser mudar.
	Bruto []byte
}

// Catalogo lista ofertas da fonte com a credencial do app.
type Catalogo interface {
	Fonte() Fonte
	Ofertas(ctx context.Context, f FiltroCatalogo) (PaginaCatalogo, error)
	// OfertaPorItem busca um produto pelo ID na fonte. Devolve
	// ErrNaoEncontrado se a fonte não o tiver.
	OfertaPorItem(ctx context.Context, itemID int64) (Oferta, error)
}

// Afiliador gera links de afiliado com a credencial de cada usuário.
type Afiliador interface {
	// Conectado diz se o usuário tem credencial válida conectada.
	Conectado(ctx context.Context, usuarioID uuid.UUID) (bool, error)
	// GerarLink devolve o link curto de afiliado para a página `origem`, com
	// os subIds informados. Devolve ErrSemCredencial sem credencial.
	GerarLink(ctx context.Context, usuarioID uuid.UUID, origem string, subIDs []string) (string, error)
}

var (
	// ErrLimite: a fonte, ou o nosso rate limit por credencial, pediu para
	// esperar. Quem chama tenta de novo mais tarde.
	ErrLimite = errors.New("limite de chamadas da fonte")
	// ErrCredencialInvalida: a fonte recusou a credencial (assinatura).
	ErrCredencialInvalida = errors.New("credencial recusada pela fonte")
	// ErrAcessoNegado: credencial válida, mas sem acesso (conta bloqueada,
	// permissão revogada).
	ErrAcessoNegado = errors.New("acesso negado pela fonte")
	// ErrIndisponivel: erro da fonte ou de rede.
	ErrIndisponivel = errors.New("fonte indisponível")
	// ErrNaoEncontrado: a fonte não tem o produto pedido.
	ErrNaoEncontrado = errors.New("produto não encontrado na fonte")
	// ErrSemCredencial: o usuário não tem credencial conectada na fonte.
	ErrSemCredencial = errors.New("usuário sem credencial conectada na fonte")
)
