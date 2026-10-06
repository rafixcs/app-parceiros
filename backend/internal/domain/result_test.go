package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewPeriod(t *testing.T) {
	now := time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC) // 4/10 at 23h in Brasília
	p, err := NewPeriod("", "", now)
	if err != nil || p.From != "2026-09-05" || p.To != "2026-10-04" {
		t.Fatalf("default: %+v %v", p, err)
	}
	if !p.End.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, ResultsLocation)) || !p.Start.Equal(time.Date(2026, 9, 5, 0, 0, 0, 0, ResultsLocation)) {
		t.Fatalf("range: %v - %v", p.Start, p.End)
	}
	if p, err := NewPeriod("2026-10-01", "2026-10-01", now); err != nil || p.End.Sub(p.Start) != 24*time.Hour {
		t.Fatalf("one day: %+v %v", p, err)
	}
	for _, c := range [][2]string{{"2026-10-05", "2026-10-01"}, {"2025-01-01", "2026-10-01"}, {"yesterday", ""}, {"", "2026-13-01"}} {
		if _, err := NewPeriod(c[0], c[1], now); !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("%v: %v", c, err)
		}
	}
}
