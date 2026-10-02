// Comando parceiros: binário único do backend. O primeiro argumento escolhe o modo:
//
//	parceiros api      serve a API HTTP
//	parceiros worker   processa os jobs do River
//	parceiros migrate  aplica as migrations (goose + River) e sai
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/db"
	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/config"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/ratelimit"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/tendencias"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("encerrando com erro", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	if len(args) != 1 {
		return errors.New("uso: parceiros api|worker|migrate")
	}
	mode := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log = log.With("mode", mode, "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch mode {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		if err := jobs.Migrate(ctx, pool); err != nil {
			return err
		}
		log.Info("migrations aplicadas")
		return nil
	case "api":
		return runAPI(ctx, log, cfg, pool)
	case "worker":
		return runWorker(ctx, log, cfg, pool)
	default:
		return fmt.Errorf("modo desconhecido %q", mode)
	}
}

func novoRedis(cfg config.Config) (*redis.Client, error) {
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return redis.NewClient(redisOpts), nil
}

// novoClienteShopee cria o cliente da Open API, ou o mock em dev, com rate
// limit por credencial no Redis.
func novoClienteShopee(log *slog.Logger, cfg config.Config, rdb *redis.Client) *shopee.Cliente {
	c := shopee.Config{
		URL: cfg.ShopeeURL,
		Limitador: ratelimit.NovoRedis(rdb, "rl:", ratelimit.Taxa{
			Por: cfg.ShopeeRatePorHora, Intervalo: time.Hour, Rajada: 10,
		}),
	}
	if cfg.ShopeeModo == "mock" {
		log.Warn("shopee em modo mock: respostas gravadas, sem chamar a API")
		return shopee.NovoMock(&shopee.Mock{Evoluir: true}, c)
	}
	return shopee.NovoCliente(c)
}

// catalogoDoApp liga o cliente à credencial do app. Sem ela (fora do mock),
// devolve nil: o catálogo não é coletado e produtos colados por link que não
// estejam no catálogo não podem ser importados.
func catalogoDoApp(cfg config.Config, cliente *shopee.Cliente) fontes.Catalogo {
	switch {
	case cfg.ShopeeModo == "mock":
		return shopee.CatalogoDoApp{Cliente: cliente, Credencial: shopee.Credencial{AppID: "1", Secret: "mock"}}
	case cfg.ShopeeAppID != "" && cfg.ShopeeAppSecret != "":
		return shopee.CatalogoDoApp{Cliente: cliente, Credencial: shopee.Credencial{AppID: cfg.ShopeeAppID, Secret: cfg.ShopeeAppSecret}}
	default:
		return nil
	}
}

func runAPI(ctx context.Context, log *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	rdb, err := novoRedis(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	kek, err := crypto.NovaKEKLocal(cfg.CryptoKEKID, cfg.CryptoKEK)
	if err != nil {
		return err
	}

	verificador, err := auth.Novo(ctx, cfg.AuthMode, cfg.Env, auth.ConfigOIDC{
		Issuer:      cfg.OIDCIssuer,
		Audience:    cfg.OIDCAudience,
		JWKSURL:     cfg.OIDCJWKSURL,
		UserinfoURL: cfg.OIDCUserinfoURL,
	})
	if err != nil {
		return err
	}
	if cfg.AuthMode == "dev" {
		log.Warn("autenticação em modo dev: tokens dev:<sub> são aceitos sem assinatura")
	}

	router := httpserver.NewRouter(log, map[string]httpserver.Checker{
		"postgres": pool.Ping,
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	})
	fila, err := jobs.NewInsertClient(pool, log)
	if err != nil {
		return err
	}
	contasSvc := contas.NewService(pool, verificador, cfg.AppURL)
	clienteShopee := novoClienteShopee(log, cfg, rdb)
	credenciais := shopee.NovasCredenciais(pool, crypto.NovoCofre(kek), clienteShopee)
	produtosSvc := produtos.NewService(pool)
	radar := tendencias.NewService(pool, produtosSvc)
	colecoesSvc := colecoes.NewService(pool, produtosSvc, catalogoDoApp(cfg, clienteShopee),
		shopee.Afiliador{Credenciais: credenciais, Cliente: clienteShopee}, colecoes.FilaRiver{Client: fila}, log)
	contas.NewHandler(contasSvc, log).Rotas(router, verificador,
		shopee.NewHandler(credenciais, log).Modulo(),
		tendencias.NewHandler(radar, log).Modulo(),
		colecoes.NewHandler(colecoesSvc, log).Modulo(),
	)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}
	errCh := make(chan error, 1)
	go func() {
		log.Info("api ouvindo", "addr", cfg.HTTPAddr)
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

func runWorker(ctx context.Context, log *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	rdb, err := novoRedis(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()

	kek, err := crypto.NovaKEKLocal(cfg.CryptoKEKID, cfg.CryptoKEK)
	if err != nil {
		return err
	}

	produtosSvc := produtos.NewService(pool)
	clienteShopee := novoClienteShopee(log, cfg, rdb)
	catalogo := catalogoDoApp(cfg, clienteShopee)
	var periodicos []*river.PeriodicJob
	switch {
	case catalogo == nil:
		log.Warn("sem SHOPEE_APP_ID e SHOPEE_APP_SECRET: o catálogo não será coletado")
	case cfg.ShopeeModo == "mock":
		if err := cadastrarCategoriasMock(ctx, produtosSvc); err != nil {
			return err
		}
		periodicos = append(periodicos, produtos.PeriodicoSnapshots())
	default:
		periodicos = append(periodicos, produtos.PeriodicoSnapshots())
	}
	afiliador := shopee.Afiliador{
		Credenciais: shopee.NovasCredenciais(pool, crypto.NovoCofre(kek), clienteShopee),
		Cliente:     clienteShopee,
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.PingWorker{Log: log})
	river.AddWorker(workers, &produtos.AgendarSnapshotsWorker{
		Svc: produtosSvc, Fonte: fontes.Shopee, Paginas: cfg.ShopeePaginas,
	})
	if catalogo != nil {
		river.AddWorker(workers, &produtos.SnapshotCatalogoWorker{
			Svc: produtosSvc, Catalogo: catalogo, Storage: novoStorage(ctx, log, cfg), Log: log,
			Depois: tendencias.Enfileirar,
		})
	}
	river.AddWorker(workers, &tendencias.CalcularTendenciasWorker{
		Svc: tendencias.NewService(pool, produtosSvc), Log: log,
	})
	river.AddWorker(workers, &colecoes.GerarLinkWorker{
		Svc: colecoes.NewService(pool, produtosSvc, catalogo, afiliador, nil, log), Log: log,
	})

	client, err := jobs.NewWorkerClient(pool, workers, periodicos, log)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return err
	}
	log.Info("worker iniciado")

	<-ctx.Done()
	return client.Stop(context.Background())
}

// cadastrarCategoriasMock registra as categorias do catálogo gravado, todas
// monitoradas, para o radar local ter filtros com nome.
func cadastrarCategoriasMock(ctx context.Context, svc *produtos.Service) error {
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

// novoStorage abre o bucket das respostas brutas. Sem S3_ENDPOINT, descarta.
func novoStorage(ctx context.Context, log *slog.Logger, cfg config.Config) storage.Storage {
	if cfg.S3Endpoint == "" {
		log.Warn("sem S3_ENDPOINT: as respostas brutas da Shopee não serão guardadas")
		return storage.Descartar{}
	}
	s3, err := storage.NovoS3(storage.Config{
		Endpoint: cfg.S3Endpoint, Bucket: cfg.S3Bucket, AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey, Region: cfg.S3Region,
	})
	if err != nil {
		log.Error("bucket S3 inválido; respostas brutas não serão guardadas", "err", err)
		return storage.Descartar{}
	}
	if cfg.Env == "dev" {
		if err := s3.GarantirBucket(ctx); err != nil {
			log.Warn("não foi possível criar o bucket local", "err", err)
		}
	}
	return s3
}
