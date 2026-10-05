package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/mail"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ratelimit"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

// identityProvider is the active identity provider. internal is set only
// with AUTH_PROVIDER=internal, and then its sign-in routes are served.
type identityProvider struct {
	domain.Authenticator
	internal *service.InternalAuth
}

// newIdentityProvider builds the provider chosen by AUTH_PROVIDER. The rest
// of the application sees only domain.Authenticator.
func newIdentityProvider(ctx context.Context, log *slog.Logger, cfg Config, pool *pgxpool.Pool,
	rdb *redis.Client, mailer domain.Mailer,
) (identityProvider, error) {
	switch cfg.AuthProvider {
	case domain.AuthProviderDev:
		log.Warn("dev authentication: dev:<sub> tokens are accepted without a signature")
		return identityProvider{Authenticator: auth.Dev{}}, nil
	case domain.AuthProviderInternal:
		var authMailer domain.AuthMailer
		if mailer != nil {
			authMailer = mail.AuthMailer{Mailer: mailer, AppURL: cfg.AppURL}
		}
		limits := service.AuthLimits{
			Account: ratelimit.Attempts{Limiter: ratelimit.NewRedis(rdb, "rl:auth:account:",
				ratelimit.Rate{Per: 10, Interval: 15 * time.Minute, Burst: 10})},
			IP: ratelimit.Attempts{Limiter: ratelimit.NewRedis(rdb, "rl:auth:ip:",
				ratelimit.Rate{Per: 100, Interval: 15 * time.Minute, Burst: 50})},
		}
		svc, err := service.NewInternalAuth(repository.NewPostgresAuth(pool), auth.DefaultArgon2id, authMailer, limits, log)
		if err != nil {
			return identityProvider{}, err
		}
		log.Info("internal authentication: email and password")
		return identityProvider{Authenticator: svc, internal: svc}, nil
	default:
		oidc, err := auth.NewOIDC(ctx, auth.OIDCConfig{
			Issuer: cfg.OIDCIssuer, Audience: cfg.OIDCAudience,
			JWKSURL: cfg.OIDCJWKSURL, UserinfoURL: cfg.OIDCUserinfoURL,
		})
		if err != nil {
			return identityProvider{}, err
		}
		return identityProvider{Authenticator: oidc}, nil
	}
}
