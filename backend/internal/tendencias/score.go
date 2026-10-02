package tendencias

import (
	"math"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
)

// Entrada é o que o score precisa de um produto.
type Entrada struct {
	Vendas         int64
	ColetadoEm     time.Time
	BaseVendas     int64
	BaseColetadoEm time.Time
	ComissaoBP     int32
	Nota           *float64
}

// Vendas7d estima o crescimento de vendas em 7 dias a partir da base. Com
// menos de um dia entre a base e a coleta atual, não há como estimar (nil).
// Com mais de 7 dias, a diferença é proporcionalizada para 7.
func Vendas7d(e Entrada) *int64 {
	dias := e.ColetadoEm.Sub(e.BaseColetadoEm).Hours() / 24
	if dias < 1 {
		return nil
	}
	delta := max(0, e.Vendas-e.BaseVendas)
	v := int64(math.Round(float64(delta) * 7 / dias))
	return &v
}

// Bruto é o score antes da normalização: o crescimento de vendas em 7 dias
// (em escala log, para um produto viral não achatar o resto), ponderado pela
// comissão e pela nota.
//
//   - comissão: de 0,5× (0%) a 1,5× (30% ou mais);
//   - nota: de 0,5× (0 estrelas) a 1× (5 estrelas); sem avaliações, 0,75×.
func Bruto(v7 *int64, comissaoBP int32, nota *float64) float64 {
	if v7 == nil || *v7 <= 0 {
		return 0
	}
	fc := 0.5 + float64(min(max(comissaoBP, 0), 3000))/3000
	fn := 0.75
	if nota != nil {
		fn = 0.5 + 0.5*min(max(*nota, 0), 5)/5
	}
	return math.Log1p(float64(*v7)) * fc * fn
}

// Normalizar leva os brutos para 0 a 100, relativo ao maior, com uma casa.
func Normalizar(brutos []float64) []float64 {
	maior := 0.0
	for _, b := range brutos {
		maior = max(maior, b)
	}
	out := make([]float64, len(brutos))
	if maior == 0 {
		return out
	}
	for i, b := range brutos {
		out[i] = math.Round(1000*b/maior) / 10
	}
	return out
}

// GanhoPorVenda é o preço × a comissão, em centavos, arredondado.
func GanhoPorVenda(precoCentavos int64, comissaoBP int32) int64 {
	return produtos.GanhoPorVenda(precoCentavos, comissaoBP)
}
