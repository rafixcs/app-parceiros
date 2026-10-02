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

// provedor simula o Zitadel: publica as chaves (JWKS) e o userinfo.
type provedor struct {
	srv   *httptest.Server
	chave *rsa.PrivateKey
}

func novoProvedor(t *testing.T) *provedor {
	t.Helper()
	chave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &provedor{chave: chave}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &chave.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/oidc/v1/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "ana@exemplo.com", "email_verified": true, "name": "Ana"})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *provedor) token(t *testing.T, aud string, exp time.Time, extra map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.chave},
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
	p := novoProvedor(t)
	ctx := context.Background()
	v, err := NovoOIDC(ctx, ConfigOIDC{Issuer: p.srv.URL, Audience: "projeto"})
	if err != nil {
		t.Fatal(err)
	}

	id, err := v.Verificar(ctx, p.token(t, "projeto", time.Now().Add(time.Hour), nil))
	if err != nil {
		t.Fatal(err)
	}
	if id.Sub != "sub-123" || id.Email != "" {
		t.Fatalf("identidade = %+v", id)
	}

	// Sem e-mail no token, o perfil vem do userinfo.
	id.token = "qualquer"
	perfil, err := v.Perfil(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if perfil.Email != "ana@exemplo.com" || !perfil.EmailVerificado || perfil.Nome != "Ana" {
		t.Fatalf("perfil = %+v", perfil)
	}

	invalidos := map[string]string{
		"outra audiência": p.token(t, "outro", time.Now().Add(time.Hour), nil),
		"expirado":        p.token(t, "projeto", time.Now().Add(-time.Minute), nil),
		"lixo":            "abc.def.ghi",
	}
	for nome, tok := range invalidos {
		if _, err := v.Verificar(ctx, tok); err == nil {
			t.Errorf("%s: token aceito", nome)
		}
	}
}

func TestDev(t *testing.T) {
	if _, err := Novo(context.Background(), "dev", "prod", ConfigOIDC{}); err == nil {
		t.Fatal("modo dev aceito fora de APP_ENV=dev")
	}
	v, err := Novo(context.Background(), "dev", "dev", ConfigOIDC{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := v.Verificar(context.Background(), "dev:ana")
	if err != nil || id.Sub != "ana" || id.Email != "ana@dev.local" {
		t.Fatalf("id = %+v, err = %v", id, err)
	}
	for _, tok := range []string{"ana", "dev:", "dev:a b"} {
		if _, err := v.Verificar(context.Background(), tok); err == nil {
			t.Errorf("token %q aceito", tok)
		}
	}
}

func TestMiddleware(t *testing.T) {
	h := Middleware(Dev{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := DoContexto(r.Context())
		_, _ = w.Write([]byte(id.Sub))
	}))
	for header, quer := range map[string]int{
		"":                http.StatusUnauthorized,
		"Basic abc":       http.StatusUnauthorized,
		"Bearer invalido": http.StatusUnauthorized,
		"Bearer dev:ana":  http.StatusOK,
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != quer {
			t.Errorf("Authorization %q: status %d, quer %d", header, rec.Code, quer)
		}
	}
}
