// Package auth holds the identity providers behind domain.Authenticator: the
// external OIDC provider (Zitadel), the dev provider and the password hasher
// used by the internal provider (service.InternalAuth).
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// OIDCConfig describes the provider (Zitadel). JWKSURL and UserinfoURL, when
// empty, follow Zitadel's default paths under the Issuer.
type OIDCConfig struct {
	Issuer      string
	Audience    string
	JWKSURL     string
	UserinfoURL string
}

// OIDC validates JWT access tokens signed by the provider. The keys are
// fetched on demand and cached by go-oidc.
type OIDC struct {
	verifier    *oidc.IDTokenVerifier
	userinfoURL string
	http        *http.Client
}

var _ domain.Authenticator = (*OIDC)(nil)

func NewOIDC(ctx context.Context, c OIDCConfig) (*OIDC, error) {
	if c.Issuer == "" || c.Audience == "" {
		return nil, fmt.Errorf("OIDC_ISSUER and OIDC_AUDIENCE are required")
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
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

func (o *OIDC) Authenticate(ctx context.Context, token string) (domain.Identity, error) {
	t, err := o.verifier.Verify(ctx, token)
	if err != nil {
		return domain.Identity{}, fmt.Errorf("%w: %v", domain.ErrInvalidToken, err)
	}
	var c claims
	if err := t.Claims(&c); err != nil {
		return domain.Identity{}, fmt.Errorf("%w: %v", domain.ErrInvalidToken, err)
	}
	return domain.Identity{
		Provider: domain.AuthProviderOIDC, Subject: t.Subject,
		Email: c.Email, EmailVerified: c.EmailVerified, Name: c.Name, Token: token,
	}, nil
}

func (o *OIDC) Profile(ctx context.Context, id domain.Identity) (domain.Profile, error) {
	if id.Email != "" && id.Name != "" {
		return domain.Profile{Email: id.Email, EmailVerified: id.EmailVerified, Name: id.Name}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.userinfoURL, nil)
	if err != nil {
		return domain.Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+id.Token)
	resp, err := o.http.Do(req)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("calling userinfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return domain.Profile{}, fmt.Errorf("userinfo answered %d", resp.StatusCode)
	}
	var c claims
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return domain.Profile{}, fmt.Errorf("reading userinfo: %w", err)
	}
	if c.Email == "" {
		return domain.Profile{}, fmt.Errorf("userinfo has no email; request the email scope at sign-in")
	}
	return domain.Profile(c), nil
}
