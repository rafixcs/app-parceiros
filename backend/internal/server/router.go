package server

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
)

// newRouter registers every route: health, the sign-in routes of the
// internal provider (when active) and the features behind the identity check.
func newRouter(log *slog.Logger, checks map[string]httpapi.Checker, identity identityProvider, s *services) chi.Router {
	router := httpapi.NewRouter(log, checks)
	if identity.internal != nil {
		httpapi.NewAuthHandler(identity.internal, log).Routes(router)
	}
	httpapi.NewAccountHandler(s.accounts, log).Mount(router, identity,
		httpapi.NewTrendHandler(s.trends, log).Routes(),
		httpapi.NewNotificationHandler(s.notifications, log).Routes(),
		httpapi.NewBillingHandler(s.billing, log).Routes(),
		httpapi.NewShopeeHandler(s.shopeeCredentials, log).Routes(),
		httpapi.NewCollectionHandler(s.collections, log).Routes(),
		httpapi.NewMediaHandler(s.media, log).Routes(),
		httpapi.NewResultHandler(s.results, log).Routes(),
		httpapi.NewCurationHandler(s.curation, log).Routes(),
	)
	return router
}
