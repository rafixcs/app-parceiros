package server

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
)

// newRouter registers every route: health, the sign-in routes of the
// internal provider (when active) and the modules behind the identity check.
func newRouter(log *slog.Logger, pool *pgxpool.Pool, rdb *redis.Client, identity identityProvider,
	accounts *contas.Service, modules []contas.Modulo,
) chi.Router {
	router := httpapi.NewRouter(log, map[string]httpapi.Checker{
		"postgres": pinger(pool),
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	})
	if identity.internal != nil {
		httpapi.NewAuthHandler(identity.internal, log).Routes(router)
	}
	contas.NewHandler(accounts, log).Rotas(router, identity, modules...)
	return router
}
