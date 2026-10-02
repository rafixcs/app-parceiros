package shopee

import (
	"context"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
)

// CatalogoDoApp liga o Cliente à credencial do app para o catálogo global.
type CatalogoDoApp struct {
	Cliente    *Cliente
	Credencial Credencial
}

func (c CatalogoDoApp) Fonte() fontes.Fonte { return fontes.Shopee }

func (c CatalogoDoApp) Ofertas(ctx context.Context, f fontes.FiltroCatalogo) (fontes.PaginaCatalogo, error) {
	return c.Cliente.Ofertas(ctx, c.Credencial, FiltroOfertas{
		CategoriaID: f.CategoriaID, Ordem: OrdemMaisVendidos, Pagina: f.Pagina, Limite: f.Limite,
	})
}

func (c CatalogoDoApp) OfertaPorItem(ctx context.Context, itemID int64) (fontes.Oferta, error) {
	return c.Cliente.OfertaPorItem(ctx, c.Credencial, itemID)
}
