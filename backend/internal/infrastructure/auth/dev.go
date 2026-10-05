package auth

import (
	"context"
	"strings"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Dev accepts unsigned `dev:<sub>` tokens, for local development without an
// identity provider. The email becomes `<sub>@dev.local`. The server only
// enables it with APP_ENV=dev.
type Dev struct{}

var _ domain.Authenticator = Dev{}

func (Dev) Authenticate(_ context.Context, token string) (domain.Identity, error) {
	sub, ok := strings.CutPrefix(token, "dev:")
	if !ok || sub == "" || strings.ContainsAny(sub, " \t") {
		return domain.Identity{}, domain.ErrInvalidToken
	}
	return domain.Identity{
		Provider: domain.AuthProviderDev, Subject: sub,
		Email: sub + "@dev.local", EmailVerified: true, Name: sub, Token: token,
	}, nil
}

func (Dev) Profile(_ context.Context, id domain.Identity) (domain.Profile, error) {
	return domain.Profile{Email: id.Email, EmailVerified: id.EmailVerified, Name: id.Name}, nil
}
