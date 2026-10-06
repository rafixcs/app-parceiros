package domain

import (
	"testing"
	"time"
)

func TestSalesGrowth7d(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		days    float64
		base    int64
		current int64
		want    *int64
	}{
		{"no history", 0, 100, 100, nil},
		{"less than a day", 0.5, 100, 150, nil},
		{"7 days", 7, 100, 800, ptr(700)},
		{"2 days extrapolate", 2, 100, 300, ptr(700)},
		{"14 days scale down", 14, 0, 1400, ptr(700)},
		{"sales fell", 7, 500, 400, ptr(0)},
	}
	for _, c := range cases {
		got := SalesGrowth7d(TrendInput{
			Sales: c.current, CollectedAt: t0.Add(time.Duration(c.days * 24 * float64(time.Hour))),
			BaseSales: c.base, BaseCollectedAt: t0,
		})
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: %v, want %v", c.name, deref(got), deref(c.want))
		}
	}
}

func TestRawScoreWeighsCommissionAndRating(t *testing.T) {
	v := ptr(1000)
	five, three := 5.0, 3.0
	if RawScore(nil, 1000, &five) != 0 || RawScore(ptr(0), 1000, &five) != 0 {
		t.Fatal("no growth must score 0")
	}
	if !(RawScore(v, 3000, &five) > RawScore(v, 500, &five)) {
		t.Fatal("a higher commission must weigh more")
	}
	if RawScore(v, 9000, &five) != RawScore(v, 3000, &five) {
		t.Fatal("a commission above 30% must not weigh more")
	}
	if !(RawScore(v, 1000, &five) > RawScore(v, 1000, &three)) {
		t.Fatal("a higher rating must weigh more")
	}
	if !(RawScore(ptr(2000), 1000, &five) > RawScore(v, 1000, &five)) {
		t.Fatal("more sales must weigh more")
	}
}

func TestNormalizeScores(t *testing.T) {
	got := NormalizeScores([]float64{2, 1, 0})
	if got[0] != 100 || got[1] != 50 || got[2] != 0 {
		t.Fatalf("%v", got)
	}
	if z := NormalizeScores([]float64{0, 0}); z[0] != 0 {
		t.Fatalf("%v", z)
	}
}

func TestEarningsPerSale(t *testing.T) {
	if g := EarningsPerSale(12990, 1200); g != 1559 {
		t.Fatalf("R$ 129,90 × 12%% = %d cents", g)
	}
	if g := EarningsPerSale(1990, 1250); g != 249 {
		t.Fatalf("R$ 19,90 × 12,5%% = %d cents", g)
	}
}

func ptr(v int64) *int64 { return &v }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
