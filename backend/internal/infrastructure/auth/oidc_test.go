package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeProvider plays Zitadel: it publishes the keys (JWKS) and the userinfo.
type fakeProvider struct {
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/oidc/v1/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "ana@example.com", "email_verified": true, "name": "Ana"})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) token(t *testing.T, aud string, exp time.Time, extra map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key},
		(&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.Claims{Issuer: p.srv.URL, Subject: "sub-123", Audience: jwt.Audience{aud}, Expiry: jwt.NewNumericDate(exp)}
	tok, err := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestOIDC(t *testing.T) {
	p := newFakeProvider(t)
	ctx := context.Background()
	v, err := NewOIDC(ctx, OIDCConfig{Issuer: p.srv.URL, Audience: "project"})
	if err != nil {
		t.Fatal(err)
	}

	id, err := v.Authenticate(ctx, p.token(t, "project", time.Now().Add(time.Hour), nil))
	if err != nil {
		t.Fatal(err)
	}
	if id.Provider != "oidc" || id.Subject != "sub-123" || id.Email != "" || id.Token == "" {
		t.Fatalf("identity = %+v", id)
	}

	// Without email in the token, the profile comes from userinfo.
	profile, err := v.Profile(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Email != "ana@example.com" || !profile.EmailVerified || profile.Name != "Ana" {
		t.Fatalf("profile = %+v", profile)
	}

	invalid := map[string]string{
		"other audience": p.token(t, "other", time.Now().Add(time.Hour), nil),
		"expired":        p.token(t, "project", time.Now().Add(-time.Minute), nil),
		"garbage":        "abc.def.ghi",
	}
	for name, tok := range invalid {
		if _, err := v.Authenticate(ctx, tok); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func TestDev(t *testing.T) {
	id, err := Dev{}.Authenticate(context.Background(), "dev:ana")
	if err != nil || id.Provider != "dev" || id.Subject != "ana" || id.Email != "ana@dev.local" {
		t.Fatalf("id = %+v, err = %v", id, err)
	}
	for _, tok := range []string{"ana", "dev:", "dev:a b"} {
		if _, err := (Dev{}).Authenticate(context.Background(), tok); err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
}
