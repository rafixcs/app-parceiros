package tendencias

import (
	"testing"
	"time"
)

func TestVendas7d(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	casos := []struct {
		nome  string
		dias  float64
		base  int64
		atual int64
		want  *int64
	}{
		{"sem histórico", 0, 100, 100, nil},
		{"menos de um dia", 0.5, 100, 150, nil},
		{"7 dias", 7, 100, 800, ptr(700)},
		{"2 dias extrapola", 2, 100, 300, ptr(700)},
		{"14 dias proporcionaliza", 14, 0, 1400, ptr(700)},
		{"vendas caíram", 7, 500, 400, ptr(0)},
	}
	for _, c := range casos {
		got := Vendas7d(Entrada{
			Vendas: c.atual, ColetadoEm: t0.Add(time.Duration(c.dias * 24 * float64(time.Hour))),
			BaseVendas: c.base, BaseColetadoEm: t0,
		})
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: %v, quer %v", c.nome, deref(got), deref(c.want))
		}
	}
}

func TestBrutoPonderaComissaoENota(t *testing.T) {
	v := ptr(1000)
	cinco, tres := 5.0, 3.0
	if Bruto(nil, 1000, &cinco) != 0 || Bruto(ptr(0), 1000, &cinco) != 0 {
		t.Fatal("sem crescimento deve dar 0")
	}
	if !(Bruto(v, 3000, &cinco) > Bruto(v, 500, &cinco)) {
		t.Fatal("comissão maior deve pesar mais")
	}
	if Bruto(v, 9000, &cinco) != Bruto(v, 3000, &cinco) {
		t.Fatal("comissão acima de 30% não deve pesar mais")
	}
	if !(Bruto(v, 1000, &cinco) > Bruto(v, 1000, &tres)) {
		t.Fatal("nota maior deve pesar mais")
	}
	if !(Bruto(ptr(2000), 1000, &cinco) > Bruto(v, 1000, &cinco)) {
		t.Fatal("mais vendas deve pesar mais")
	}
}

func TestNormalizar(t *testing.T) {
	got := Normalizar([]float64{2, 1, 0})
	if got[0] != 100 || got[1] != 50 || got[2] != 0 {
		t.Fatalf("%v", got)
	}
	if z := Normalizar([]float64{0, 0}); z[0] != 0 {
		t.Fatalf("%v", z)
	}
}

func TestGanhoPorVenda(t *testing.T) {
	if g := GanhoPorVenda(12990, 1200); g != 1559 {
		t.Fatalf("R$ 129,90 × 12%% = %d centavos", g)
	}
	if g := GanhoPorVenda(1990, 1250); g != 249 {
		t.Fatalf("R$ 19,90 × 12,5%% = %d centavos", g)
	}
}

func ptr(v int64) *int64 { return &v }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
