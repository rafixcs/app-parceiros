// Package server wires the application: it reads the configuration, builds
// the infrastructure (database, Redis, identity provider, gateways), injects
// it into the services and runs the API or the worker. It is the only package
// that knows every concrete implementation.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/db"
	"github.com/rafixcs/app-parceiros/backend/internal/assinaturas"
	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/mail"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ratelimit"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/midia"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/resultados"
	"github.com/rafixcs/app-parceiros/backend/internal/tendencias"
)

// Migrate applies the application and River migrations.
func Migrate(ctx context.Context, log *slog.Logger, cfg Config) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	if err := queue.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

// RunAPI serves the HTTP API until ctx ends.
func RunAPI(ctx context.Context, log *slog.Logger, cfg Config) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	rdb, err := newRedis(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	kek, err := crypto.NewLocalKEK(cfg.CryptoKEKID, cfg.CryptoKEK)
	if err != nil {
		return err
	}
	mailer := newMailer(log, cfg)
	identity, err := newIdentityProvider(ctx, log, cfg, pool, rdb, mailer)
	if err != nil {
		return err
	}
	jobs, err := queue.NewInsertClient(pool, log)
	if err != nil {
		return err
	}

	accountsSvc := contas.NewService(pool, identity, cfg.AppURL)
	shopeeClient := newShopeeClient(log, cfg, rdb)
	credentials := shopee.NovasCredenciais(pool, crypto.NewVault(kek), shopeeClient)
	affiliator := shopee.Afiliador{Credenciais: credentials, Cliente: shopeeClient}
	productsSvc := produtos.NewService(pool)
	trendsSvc := tendencias.NewService(pool, productsSvc)
	collectionsSvc := colecoes.NewService(pool, productsSvc, appCatalog(cfg, shopeeClient),
		affiliator, colecoes.FilaRiver{Client: jobs}, log)
	notificationsSvc := notificacoes.NewService(pool, notificacoes.FilaRiver{Client: jobs}, accountsSvc,
		mailer, newPush(log, cfg), cfg.AppURL, log)
	accountsSvc.EnviarConvitesCom(notificationsSvc.EnviarConvite)
	mediaSvc := midia.NewService(pool, productsSvc, &midia.OEmbed{Cache: midia.CacheRedis{R: rdb}},
		mediaObjects(newBucket(ctx, log, cfg)), nil, accountsSvc, &midia.FilaRiver{Client: jobs}, log)
	curationSvc := curadoria.NewService(pool, productsSvc, collectionsSvc, accountsSvc, notificationsSvc, mediaSvc, log)
	resultsSvc := resultados.NewService(pool, shopee.Relatorio{Credenciais: credentials, Cliente: shopeeClient},
		affiliator, productsSvc, accountsSvc, curationSvc, resultados.FilaRiver{Client: jobs}, log)
	subscriptionsSvc := assinaturas.NewService(pool, newPaymentGateway(log, cfg), accountsSvc, notificationsSvc, log)

	router := newRouter(log, pool, rdb, identity, accountsSvc, []contas.Modulo{
		shopee.NewHandler(credentials, log).Modulo(),
		tendencias.NewHandler(trendsSvc, log).Modulo(),
		colecoes.NewHandler(collectionsSvc, log).Modulo(),
		curadoria.NewHandler(curationSvc, log).Modulo(),
		notificacoes.NewHandler(notificationsSvc, log).Modulo(),
		midia.NewHandler(mediaSvc, log).Modulo(),
		resultados.NewHandler(resultsSvc, log).Modulo(),
		assinaturas.NewHandler(subscriptionsSvc, log).Modulo(),
	})

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// RunWorker processes the River jobs until ctx ends.
func RunWorker(ctx context.Context, log *slog.Logger, cfg Config) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	rdb, err := newRedis(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	kek, err := crypto.NewLocalKEK(cfg.CryptoKEKID, cfg.CryptoKEK)
	if err != nil {
		return err
	}

	productsSvc := produtos.NewService(pool)
	shopeeClient := newShopeeClient(log, cfg, rdb)
	catalog := appCatalog(cfg, shopeeClient)
	var periodic []*river.PeriodicJob
	switch {
	case catalog == nil:
		log.Warn("no SHOPEE_APP_ID and SHOPEE_APP_SECRET: the catalog will not be collected")
	case cfg.ShopeeMode == "mock":
		if err := registerMockCategories(ctx, productsSvc); err != nil {
			return err
		}
		periodic = append(periodic, produtos.PeriodicoSnapshots())
	default:
		periodic = append(periodic, produtos.PeriodicoSnapshots())
	}
	credentials := shopee.NovasCredenciais(pool, crypto.NewVault(kek), shopeeClient)
	affiliator := shopee.Afiliador{Credenciais: credentials, Cliente: shopeeClient}
	periodic = append(periodic, resultados.PeriodicoSync())

	workers := river.NewWorkers()
	river.AddWorker(workers, &queue.PingWorker{Log: log})
	river.AddWorker(workers, &produtos.AgendarSnapshotsWorker{
		Svc: productsSvc, Fonte: fontes.Shopee, Paginas: cfg.ShopeePages,
	})
	bucket := newBucket(ctx, log, cfg)
	if catalog != nil {
		var raw storage.Storage = storage.Discard{}
		if bucket != nil {
			raw = bucket
		}
		river.AddWorker(workers, &produtos.SnapshotCatalogoWorker{
			Svc: productsSvc, Catalogo: catalog, Storage: raw, Log: log,
			Depois: tendencias.Enfileirar,
		})
	}
	river.AddWorker(workers, &tendencias.CalcularTendenciasWorker{
		Svc: tendencias.NewService(pool, productsSvc), Log: log,
	})
	river.AddWorker(workers, &colecoes.GerarLinkWorker{
		Svc: colecoes.NewService(pool, productsSvc, catalog, affiliator, nil, log), Log: log,
	})
	accountsSvc := contas.NewService(pool, nil, cfg.AppURL) // only reads contacts
	river.AddWorker(workers, &notificacoes.EntregarWorker{
		Svc: notificacoes.NewService(pool, nil, accountsSvc, newMailer(log, cfg), newPush(log, cfg), cfg.AppURL, log),
	})
	// Revalidation schedules its next round through the queue, which gets the
	// client right below, before the worker starts.
	mediaQueue := &midia.FilaRiver{}
	mediaSvc := midia.NewService(pool, productsSvc, &midia.OEmbed{}, mediaObjects(bucket), midia.FFmpeg{}, accountsSvc, mediaQueue, log)
	river.AddWorker(workers, &midia.ProcessarVideoWorker{Svc: mediaSvc, Log: log})
	river.AddWorker(workers, &midia.RevalidarEmbedWorker{Svc: mediaSvc})
	river.AddWorker(workers, &midia.LimparUploadWorker{Svc: mediaSvc})
	river.AddWorker(workers, &resultados.AgendarSyncWorker{Usuarios: credentials})
	river.AddWorker(workers, &resultados.SyncConversoesWorker{
		Svc: resultados.NewService(pool, shopee.Relatorio{Credenciais: credentials, Cliente: shopeeClient},
			affiliator, productsSvc, accountsSvc, nil, nil, log),
		Log: log,
	})

	client, err := queue.NewWorkerClient(pool, workers, periodic, log)
	if err != nil {
		return err
	}
	mediaQueue.Client = client
	if err := client.Start(ctx); err != nil {
		return err
	}
	log.Info("worker started")

	<-ctx.Done()
	return client.Stop(context.Background())
}

func newRedis(cfg Config) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
	}
	return redis.NewClient(opts), nil
}

// newShopeeClient creates the Open API client, or the mock in dev, with a
// rate limit per credential in Redis.
func newShopeeClient(log *slog.Logger, cfg Config, rdb *redis.Client) *shopee.Cliente {
	c := shopee.Config{
		URL: cfg.ShopeeURL,
		Limitador: ratelimit.NewRedis(rdb, "rl:", ratelimit.Rate{
			Per: cfg.ShopeeRatePerHour, Interval: time.Hour, Burst: 10,
		}),
	}
	if cfg.ShopeeMode == "mock" {
		log.Warn("shopee in mock mode: recorded answers, no API calls")
		return shopee.NovoMock(&shopee.Mock{Evoluir: true}, c)
	}
	return shopee.NovoCliente(c)
}

// appCatalog binds the client to the app credential. Without it (outside the
// mock) it returns nil: the catalog is not collected, and products pasted by
// link that are not in the catalog cannot be imported.
func appCatalog(cfg Config, client *shopee.Cliente) fontes.Catalogo {
	switch {
	case cfg.ShopeeMode == "mock":
		return shopee.CatalogoDoApp{Cliente: client, Credencial: shopee.Credencial{AppID: "1", Secret: "mock"}}
	case cfg.ShopeeAppID != "" && cfg.ShopeeAppSecret != "":
		return shopee.CatalogoDoApp{Cliente: client, Credencial: shopee.Credencial{AppID: cfg.ShopeeAppID, Secret: cfg.ShopeeAppSecret}}
	default:
		return nil
	}
}

// newPaymentGateway picks Asaas or, in dev, the mock, which charges nothing
// and accepts simulated payments.
func newPaymentGateway(log *slog.Logger, cfg Config) assinaturas.Gateway {
	if cfg.BillingMode == "mock" {
		log.Warn("billing in mock mode: nothing is charged and payments are simulated")
		return assinaturas.NovoMock()
	}
	if cfg.AsaasWebhookSecret == "" {
		log.Warn("no ASAAS_WEBHOOK_SECRET: Asaas billing notices will be refused")
	}
	return assinaturas.Asaas{URL: cfg.AsaasURL, Chave: cfg.AsaasAPIKey, SegredoWebhook: cfg.AsaasWebhookSecret}
}

// newMailer returns the SMTP mailer, or nil without SMTP_ADDR.
func newMailer(log *slog.Logger, cfg Config) domain.Mailer {
	if cfg.SMTPAddr == "" {
		log.Warn("no SMTP_ADDR: no email will be sent")
		return nil
	}
	return mail.SMTP{Addr: cfg.SMTPAddr, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
}

// newPush returns Web Push, or nil without the VAPID keys.
func newPush(log *slog.Logger, cfg Config) notificacoes.Push {
	if cfg.VAPIDPublicKey == "" {
		log.Warn("no VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY: no Web Push notifications")
		return nil
	}
	return notificacoes.WebPush{Publica: cfg.VAPIDPublicKey, Privada: cfg.VAPIDPrivateKey, Assunto: cfg.VAPIDSubject}
}

// registerMockCategories registers the categories of the recorded catalog,
// all monitored, so the local radar has named filters.
func registerMockCategories(ctx context.Context, svc *produtos.Service) error {
	cats, err := shopee.Categorias()
	if err != nil {
		return err
	}
	out := make([]produtos.Categoria, len(cats))
	for i, c := range cats {
		out[i] = produtos.Categoria{ID: c.ID, Nome: c.Nome, Monitorar: true}
	}
	return svc.SalvarCategorias(ctx, fontes.Shopee, out)
}

// newBucket opens the S3 bucket (raw Shopee answers and videos). Without
// S3_ENDPOINT it returns nil.
func newBucket(ctx context.Context, log *slog.Logger, cfg Config) *storage.S3 {
	if cfg.S3Endpoint == "" {
		log.Warn("no S3_ENDPOINT: no bucket for videos and raw answers")
		return nil
	}
	s3, err := storage.NewS3(storage.Config{
		Endpoint: cfg.S3Endpoint, PublicEndpoint: cfg.S3PublicEndpoint, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Region: cfg.S3Region,
	})
	if err != nil {
		log.Error("invalid S3 bucket; no videos nor raw answers", "err", err)
		return nil
	}
	if cfg.Env == "dev" {
		if err := s3.EnsureBucket(ctx); err != nil {
			log.Warn("could not create the local bucket", "err", err)
		}
	}
	return s3
}

// mediaObjects avoids passing a nil *storage.S3 as a non-nil interface.
func mediaObjects(s3 *storage.S3) midia.Objetos {
	if s3 == nil {
		return nil
	}
	return s3
}

// pinger adapts a pool to the readiness check.
func pinger(pool *pgxpool.Pool) func(context.Context) error { return pool.Ping }
