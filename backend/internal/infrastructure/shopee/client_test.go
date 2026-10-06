package shopee

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestSign(t *testing.T) {
	// Vector computed outside Go (python hashlib): sha256("123" + "1700000000" + `{"query":"x"}` + "abc").
	got := Sign(Credential{AppID: "123", Secret: "abc"}, time.Unix(1700000000, 0), []byte(`{"query":"x"}`))
	want := "SHA256 Credential=123, Timestamp=1700000000, Signature=09f81a15291db81967cc0fca18bbc9144059c8ccac44d050e5ce2749f509529d"
	if got != want {
		t.Fatalf("signature %q, want %q", got, want)
	}
}

func TestDecimal(t *testing.T) {
	cases := []struct {
		in     string
		places int
		want   int64
	}{
		{"129.9", 2, 12990}, {"129.90", 2, 12990}, {"19", 2, 1900}, {"0.125", 4, 1250},
		{"0.1", 4, 1000}, {"4.85", 2, 485}, {"0.12345", 4, 1235}, {"", 2, 0}, {"1500", 0, 1500},
	}
	for _, c := range cases {
		got, err := decimal(c.in).scale(c.places)
		if err != nil || got != c.want {
			t.Errorf("%q/%d: %d %v, want %d", c.in, c.places, got, err, c.want)
		}
	}
	for _, bad := range []string{"abc", "1e5", "1.2.3"} {
		if _, err := decimal(bad).scale(2); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestOffersMock(t *testing.T) {
	ctx := context.Background()
	m := &Mock{Secrets: map[string]string{"111": "secret"}}
	c := NewMock(m, Config{})
	cred := Credential{AppID: "111", Secret: "secret"}

	p, err := c.Offers(ctx, cred, OfferFilter{CategoryID: 100004, Page: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Offers) != 10 || !p.HasNext || len(p.Raw) == 0 {
		t.Fatalf("page 1: %d offers, next %v", len(p.Offers), p.HasNext)
	}
	for i, o := range p.Offers {
		if o.Categories[0] != 100004 || o.ItemID == 0 || o.MinPriceCents == 0 || o.CommissionBP == 0 || o.URL == "" {
			t.Fatalf("offer %d incomplete: %+v", i, o)
		}
		if i > 0 && o.Sales > p.Offers[i-1].Sales {
			t.Fatal("not sorted by best sellers")
		}
	}
	p2, err := c.Offers(ctx, cred, OfferFilter{CategoryID: 100004, Page: 2, Limit: 10})
	if err != nil || len(p2.Offers) != 6 || p2.HasNext {
		t.Fatalf("page 2: %d %v %v", len(p2.Offers), p2.HasNext, err)
	}

	if err := c.Validate(ctx, Credential{AppID: "111", Secret: "other"}); !errors.Is(err, domain.ErrSourceInvalidCredential) {
		t.Fatalf("wrong secret: %v", err)
	}
}

func TestPageLimit(t *testing.T) {
	c := NewMock(&Mock{}, Config{})
	p, err := c.Offers(context.Background(), Credential{AppID: "1", Secret: "x"}, OfferFilter{Limit: 500})
	if err != nil || len(p.Offers) > PageLimit {
		t.Fatalf("%d offers, %v", len(p.Offers), err)
	}
}

func TestAPIErrors(t *testing.T) {
	ctx := context.Background()
	c := NewMock(&Mock{}, Config{})
	cases := map[string]error{
		MockAppIDInvalid:      domain.ErrSourceInvalidCredential,
		MockAppIDRateLimited:  domain.ErrSourceLimit,
		MockAppIDAccessDenied: domain.ErrSourceAccessDenied,
		MockAppIDUnavailable:  domain.ErrSourceUnavailable,
	}
	for app, want := range cases {
		err := c.Validate(ctx, Credential{AppID: app, Secret: "s3cr3t-n0t-l34k3d"})
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", app, err, want)
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: secret in the error", app)
		}
	}
	if err := c.Validate(ctx, Credential{AppID: "999", Secret: "x"}); err != nil {
		t.Fatalf("any AppID: %v", err)
	}
}

type fullLimiter struct{}

func (fullLimiter) Reserve(context.Context, string) (time.Duration, error) { return time.Hour, nil }

func TestRateLimitPerCredential(t *testing.T) {
	m := &Mock{}
	c := NewMock(m, Config{Limiter: fullLimiter{}, MaxWait: time.Second})
	err := c.Validate(context.Background(), Credential{AppID: "1", Secret: "x"})
	if !errors.Is(err, domain.ErrSourceLimit) || m.Calls() != 0 {
		t.Fatalf("%v, %d calls", err, m.Calls())
	}
}

func TestCredentialHidesSecret(t *testing.T) {
	cred := Credential{AppID: "1", Secret: "s3cr3t"}
	for _, s := range []string{cred.String(), cred.GoString()} {
		if strings.Contains(s, "s3cr3t") {
			t.Fatalf("secret in %q", s)
		}
	}
}

func TestMockEvolves(t *testing.T) {
	ctx := context.Background()
	now := recordedAt
	m := &Mock{Evolve: true, Now: func() time.Time { return now }}
	c := NewMock(m, Config{})
	cred := Credential{AppID: "1", Secret: "x"}
	before, err := c.Offers(ctx, cred, OfferFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * 24 * time.Hour)
	after, _ := c.Offers(ctx, cred, OfferFilter{Limit: 50})
	var total0, total1 int64
	for i := range before.Offers {
		total0 += before.Offers[i].Sales
		total1 += after.Offers[i].Sales
	}
	if total1 <= total0 {
		t.Fatalf("sales did not grow: %d -> %d", total0, total1)
	}
}

func TestCategories(t *testing.T) {
	cs, err := Categories()
	if err != nil || len(cs) == 0 || cs[0].Name == "" || !cs[0].Monitored {
		t.Fatalf("%+v %v", cs, err)
	}
}
