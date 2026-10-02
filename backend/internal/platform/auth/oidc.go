package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// ConfigOIDC descreve o provedor (Zitadel). JWKSURL e UserinfoURL, quando
// vazios, seguem os caminhos padrão do Zitadel a partir do Issuer.
type ConfigOIDC struct {
	Issuer      string
	Audience    string
	JWKSURL     string
	UserinfoURL string
}

// OIDC valida tokens de acesso JWT assinados pelo provedor. As chaves são
// buscadas sob demanda e mantidas em cache pelo go-oidc.
type OIDC struct {
	verifier    *oidc.IDTokenVerifier
	userinfoURL string
	http        *http.Client
}

func NovoOIDC(ctx context.Context, c ConfigOIDC) (*OIDC, error) {
	if c.Issuer == "" || c.Audience == "" {
		return nil, fmt.Errorf("OIDC_ISSUER e OIDC_AUDIENCE são obrigatórias")
	}
	issuer := strings.TrimRight(c.Issuer, "/")
	if c.JWKSURL == "" {
		c.JWKSURL = issuer + "/oauth/v2/keys"
	}
	if c.UserinfoURL == "" {
		c.UserinfoURL = issuer + "/oidc/v1/userinfo"
	}
	keys := oidc.NewRemoteKeySet(ctx, c.JWKSURL)
	return &OIDC{
		verifier:    oidc.NewVerifier(c.Issuer, keys, &oidc.Config{ClientID: c.Audience}),
		userinfoURL: c.UserinfoURL,
		http:        &http.Client{Timeout: 5 * time.Second},
	}, nil
}

type claims struct {
	Email           string `json:"email"`
	EmailVerificado bool   `json:"email_verified"`
	Nome            string `json:"name"`
}

func (o *OIDC) Verificar(ctx context.Context, token string) (Identidade, error) {
	t, err := o.verifier.Verify(ctx, token)
	if err != nil {
		return Identidade{}, fmt.Errorf("%w: %v", ErrTokenInvalido, err)
	}
	var c claims
	if err := t.Claims(&c); err != nil {
		return Identidade{}, fmt.Errorf("%w: %v", ErrTokenInvalido, err)
	}
	return Identidade{Sub: t.Subject, Email: c.Email, EmailVerificado: c.EmailVerificado, Nome: c.Nome}, nil
}

func (o *OIDC) Perfil(ctx context.Context, id Identidade) (Perfil, error) {
	if id.Email != "" && id.Nome != "" {
		return Perfil{Email: id.Email, EmailVerificado: id.EmailVerificado, Nome: id.Nome}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.userinfoURL, nil)
	if err != nil {
		return Perfil{}, err
	}
	req.Header.Set("Authorization", "Bearer "+id.token)
	resp, err := o.http.Do(req)
	if err != nil {
		return Perfil{}, fmt.Errorf("consultando userinfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Perfil{}, fmt.Errorf("userinfo respondeu %d", resp.StatusCode)
	}
	var c claims
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return Perfil{}, fmt.Errorf("lendo userinfo: %w", err)
	}
	if c.Email == "" {
		return Perfil{}, fmt.Errorf("userinfo sem e-mail; inclua o escopo email no login")
	}
	return Perfil(c), nil
}
