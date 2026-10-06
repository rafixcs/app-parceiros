package server

import (
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ratelimit"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
)

// withShopee fills in the Shopee client, the app catalog and the vault of
// the users' secrets. RunAPI and RunWorker both use it.
func withShopee(in *infra, cfg Config, rdb *redis.Client) error {
	vault, err := newVault(cfg)
	if err != nil {
		return err
	}
	in.secrets = vault
	in.shopee = newShopeeClient(in.log, cfg, rdb)
	in.catalog = appCatalog(cfg, in.shopee)
	return nil
}

// newVault opens the vault of the users' secrets with the master key
// CRYPTO_KEK.
func newVault(cfg Config) (*crypto.Vault, error) {
	kek, err := crypto.NewLocalKEK(cfg.CryptoKEKID, cfg.CryptoKEK)
	if err != nil {
		return nil, err
	}
	return crypto.NewVault(kek), nil
}

// newShopeeClient creates the Open API client, or the mock in dev, with a
// rate limit per credential in Redis.
func newShopeeClient(log *slog.Logger, cfg Config, rdb *redis.Client) *shopee.Client {
	c := shopee.Config{
		URL: cfg.ShopeeURL,
		Limiter: ratelimit.NewRedis(rdb, "rl:", ratelimit.Rate{
			Per: cfg.ShopeeRatePerHour, Interval: time.Hour, Burst: 10,
		}),
	}
	if cfg.ShopeeMode == "mock" {
		log.Warn("shopee in mock mode: recorded answers, no API calls")
		return shopee.NewMock(&shopee.Mock{Evolve: true}, c)
	}
	return shopee.NewClient(c)
}

// appCatalog binds the client to the app credential. Without it (outside the
// mock) it returns nil: the catalog is not collected, and products pasted by
// link that are not in the catalog cannot be imported.
func appCatalog(cfg Config, client *shopee.Client) domain.Catalog {
	switch {
	case cfg.ShopeeMode == "mock":
		return shopee.AppCatalog{Client: client, Credential: shopee.Credential{AppID: "1", Secret: "mock"}}
	case cfg.ShopeeAppID != "" && cfg.ShopeeAppSecret != "":
		return shopee.AppCatalog{Client: client, Credential: shopee.Credential{AppID: cfg.ShopeeAppID, Secret: cfg.ShopeeAppSecret}}
	default:
		return nil
	}
}
