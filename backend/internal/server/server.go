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

	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/db"
	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/billing"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ffmpeg"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/mail"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/oembed"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/push"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
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

	mailer := newMailer(log, cfg)
	identity, err := newIdentityProvider(ctx, log, cfg, pool, rdb, mailer)
	if err != nil {
		return err
	}
	jobs, err := queue.NewInsertClient(pool, log)
	if err != nil {
		return err
	}
	in := infra{
		log: log, pool: pool, appURL: cfg.AppURL, profiles: identity, mailer: mailer,
		notificationQueue: &queue.River{Client: jobs}, linkQueue: &queue.River{Client: jobs}, push: newPush(log, cfg),
		objects:    objectStore(newBucket(ctx, log, cfg)),
		embeds:     &oembed.Client{Cache: oembed.RedisCache{R: rdb}},
		mediaQueue: &queue.River{Client: jobs}, conversionSyncQueue: &queue.River{Client: jobs},
		payments: newPaymentGateway(log, cfg),
	}
	if err := withShopee(&in, cfg, rdb); err != nil {
		return err
	}
	svcs := newServices(in)
	router := newRouter(log, map[string]httpapi.Checker{
		"postgres": pool.Ping,
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}, identity, svcs)

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

	// Jobs enqueue other jobs with the River client of the job being worked.
	jobs := &queue.River{}
	in := infra{
		log: log, pool: pool, appURL: cfg.AppURL, mailer: newMailer(log, cfg),
		notificationQueue: jobs, trendQueue: jobs, linkQueue: jobs, mediaQueue: jobs, push: newPush(log, cfg),
		videoProcessor: ffmpeg.Processor{},
	}
	if bucket := newBucket(ctx, log, cfg); bucket != nil {
		in.objects, in.raw = bucket, bucket
	}
	if err := withShopee(&in, cfg, rdb); err != nil {
		return err
	}
	svcs := newServices(in)

	var periodic []*river.PeriodicJob
	workers := river.NewWorkers()
	river.AddWorker(workers, &queue.PingWorker{Log: log})
	river.AddWorker(workers, &queue.ScheduleSnapshotsWorker{
		Svc: svcs.products, Source: domain.SourceShopee, Pages: cfg.ShopeePages, Queue: jobs,
	})
	if in.catalog == nil {
		log.Warn("no SHOPEE_APP_ID and SHOPEE_APP_SECRET: the catalog will not be collected")
	} else {
		if cfg.ShopeeMode == "mock" {
			// The recorded catalog's categories, all monitored, so the local
			// radar has named filters.
			cats, err := shopee.Categories()
			if err != nil {
				return err
			}
			if err := svcs.products.SaveCategories(ctx, domain.SourceShopee, cats); err != nil {
				return err
			}
		}
		river.AddWorker(workers, &queue.SnapshotCatalogWorker{Svc: svcs.products, Log: log})
		periodic = append(periodic, queue.PeriodicSnapshots(service.SnapshotInterval))
	}
	river.AddWorker(workers, &queue.ComputeTrendsWorker{Svc: svcs.trends, Log: log})
	river.AddWorker(workers, &queue.DeliverNotificationWorker{Service: svcs.notifications})
	river.AddWorker(workers, &queue.GenerateAffiliateLinkWorker{Svc: svcs.collections, Log: log})
	river.AddWorker(workers, &queue.ProcessVideoWorker{Svc: svcs.media, Log: log})
	river.AddWorker(workers, &queue.RevalidateEmbedWorker{Svc: svcs.media})
	river.AddWorker(workers, &queue.CleanUploadWorker{Svc: svcs.media})
	river.AddWorker(workers, &queue.SyncConversionsWorker{Svc: svcs.results, Log: log})
	river.AddWorker(workers, &queue.ScheduleConversionSyncsWorker{Users: svcs.shopeeCredentials, Queue: jobs})
	periodic = append(periodic, queue.PeriodicConversionSyncs())

	client, err := queue.NewWorkerClient(pool, workers, periodic, log)
	if err != nil {
		return err
	}
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

// newMailer returns the SMTP mailer, or nil without SMTP_ADDR.
func newMailer(log *slog.Logger, cfg Config) domain.Mailer {
	if cfg.SMTPAddr == "" {
		log.Warn("no SMTP_ADDR: no email will be sent")
		return nil
	}
	return mail.SMTP{Addr: cfg.SMTPAddr, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
}

// newPush returns Web Push, or nil without the VAPID keys.
func newPush(log *slog.Logger, cfg Config) domain.PushSender {
	if cfg.VAPIDPublicKey == "" {
		log.Warn("no VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY: no Web Push notifications")
		return nil
	}
	return push.WebPush{PublicKeyB64: cfg.VAPIDPublicKey, PrivateKeyB64: cfg.VAPIDPrivateKey, Subject: cfg.VAPIDSubject}
}

// newPaymentGateway picks Asaas or, in dev, the mock, which charges nothing
// and lets the payment be simulated.
func newPaymentGateway(log *slog.Logger, cfg Config) domain.PaymentGateway {
	if cfg.BillingMode == "mock" {
		log.Warn("billing in mock mode: nothing is charged and payments are simulated")
		return billing.NewMock()
	}
	if cfg.AsaasWebhookSecret == "" {
		log.Warn("no ASAAS_WEBHOOK_SECRET: Asaas billing events will be refused")
	}
	return billing.Asaas{URL: cfg.AsaasURL, APIKey: cfg.AsaasAPIKey, WebhookSecret: cfg.AsaasWebhookSecret}
}

// newBucket opens the S3 bucket (videos and raw answers of the sources).
// Without S3_ENDPOINT it returns nil.
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

// objectStore avoids passing a nil *storage.S3 as a non-nil interface.
func objectStore(s3 *storage.S3) domain.ObjectStore {
	if s3 == nil {
		return nil
	}
	return s3
}
