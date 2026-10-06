package server

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/mail"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/oembed"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

// infra is the infrastructure the services are built on. RunAPI and
// RunWorker fill it from the configuration; tests fill it with fakes.
type infra struct {
	log    *slog.Logger
	pool   *pgxpool.Pool
	appURL string
	// profiles fetches the profile of a new user; nil in the worker.
	profiles service.ProfileSource
	// mailer sends emails; nil sends none.
	mailer domain.Mailer
	// raw keeps the raw answers of the source; nil keeps none.
	raw domain.RawStore
	// trendQueue schedules the radar after a collection; nil in the API.
	trendQueue domain.TrendQueue
	// notificationQueue enqueues deliver_notification (River; nil in the
	// worker, which does not enqueue).
	notificationQueue domain.NotificationQueue
	// push sends Web Push; nil sends none.
	push domain.PushSender
	// payments is the payment gateway (Asaas, or the mock in dev); nil
	// answers billing_unavailable.
	payments domain.PaymentGateway
	// shopee is the Open API client (the mock with SHOPEE_MODE=mock).
	shopee *shopee.Client
	// catalog is the Shopee catalog with the app credential; nil without it
	// (outside the mock): the catalog is not collected and pasted links of
	// products not collected yet are not imported.
	catalog domain.Catalog
	// secrets seals the users' secrets (the Shopee credential).
	secrets domain.SecretBox
	// affiliator and linkParser replace the Shopee ones (tests).
	affiliator domain.Affiliator
	linkParser domain.LinkParser
	// linkQueue enqueues generate_affiliate_link.
	linkQueue domain.AffiliateLinkQueue
	// objects is the bucket of the videos; nil allows only embeds.
	objects domain.ObjectStore
	// embeds resolves video links by oEmbed; nil uses the official endpoints
	// without cache.
	embeds domain.EmbedResolver
	// videoProcessor makes preview and thumbnail of uploads; worker only.
	videoProcessor domain.VideoProcessor
	// mediaQueue schedules the media jobs; nil schedules none.
	mediaQueue domain.MediaQueue
	// conversionSyncQueue enqueues sync_conversions; nil in the worker.
	conversionSyncQueue domain.ConversionSyncQueue
	// resultLists replaces curation as the source of the lists of the group
	// results (tests).
	resultLists resultListSource
}

// resultListSource is what the results need from curation.
type resultListSource interface {
	Imports(ctx context.Context, m domain.Member) ([]domain.ImportedList, error)
}

// services are the use cases, wired to their repositories and gateways.
type services struct {
	accounts      *service.AccountService
	products      *service.ProductService
	trends        *service.TrendService
	notifications *service.NotificationService
	billing       *service.BillingService
	collections   *service.CollectionService
	media         *service.MediaService
	results       *service.ResultService
	curation      *service.CurationService

	shopeeCredentials *service.ShopeeCredentialService
	// affiliator and conversionReport use the credential of each user.
	affiliator       domain.Affiliator
	conversionReport domain.ConversionReport
	linkParser       domain.LinkParser
}

func newServices(in infra) *services {
	tx := database.Transactor{Pool: in.pool}
	accounts := service.NewAccountService(repository.NewPostgresAccount(in.pool), tx, in.profiles, in.appURL)
	productRepo := repository.NewPostgresProduct(in.pool)
	products := service.NewProductService(productRepo, in.catalog, in.raw, in.trendQueue, in.log)
	trends := service.NewTrendService(repository.NewPostgresTrend(in.pool), productRepo)
	var notificationMailer domain.NotificationMailer
	if in.mailer != nil {
		notificationMailer = mail.NotificationMailer{Mailer: in.mailer, AppURL: in.appURL}
	}
	notifications := service.NewNotificationService(repository.NewPostgresNotification(in.pool), tx,
		in.notificationQueue, accounts, notificationMailer, in.push, in.log)
	accounts.SendInvitesWith(notifications)
	billing := service.NewBillingService(repository.NewPostgresBilling(in.pool), tx, in.payments, accounts, notifications, in.log)
	shopeeCredentials := service.NewShopeeCredentialService(
		repository.NewPostgresShopeeCredential(in.pool), in.secrets, in.shopee)
	var affiliator domain.Affiliator = shopee.Affiliator{Credentials: shopeeCredentials, Client: in.shopee}
	if in.affiliator != nil {
		affiliator = in.affiliator
	}
	var linkParser domain.LinkParser = shopee.LinkParser{}
	if in.linkParser != nil {
		linkParser = in.linkParser
	}
	collections := service.NewCollectionService(repository.NewPostgresCollection(in.pool), tx, products,
		affiliator, linkParser, in.linkQueue, in.log)
	embeds := in.embeds
	if embeds == nil {
		embeds = &oembed.Client{}
	}
	media := service.NewMediaService(repository.NewPostgresMedia(in.pool), tx, products, accounts, embeds, in.objects,
		in.videoProcessor, in.mediaQueue, in.log)
	conversionReport := shopee.Report{Credentials: shopeeCredentials, Client: in.shopee}
	curation := service.NewCurationService(repository.NewPostgresCuration(in.pool), tx, products, collections, accounts,
		notifications, media, in.log)
	var resultLists resultListSource = curation
	if in.resultLists != nil {
		resultLists = in.resultLists
	}
	results := service.NewResultService(repository.NewPostgresResult(in.pool), conversionReport, affiliator,
		products, accounts, resultLists, in.conversionSyncQueue, in.log)
	return &services{
		results:           results,
		media:             media,
		curation:          curation,
		collections:       collections,
		accounts:          accounts,
		products:          products,
		trends:            trends,
		notifications:     notifications,
		billing:           billing,
		shopeeCredentials: shopeeCredentials,
		affiliator:        affiliator,
		conversionReport:  conversionReport,
		linkParser:        linkParser,
	}
}
