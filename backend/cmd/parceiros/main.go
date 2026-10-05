// Comando parceiros: binário único do backend. O primeiro argumento escolhe o modo:
//
//	parceiros api      serve a API HTTP
//	parceiros worker   processa os jobs do River
//	parceiros migrate  aplica as migrations (goose + River) e sai
//	parceiros vapid    gera um par de chaves VAPID para o Web Push e sai
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
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/midia"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/config"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/ratelimit"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
	"github.com/rafixcs/app-parceiros/backend/internal/resultados"
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
		return errors.New("uso: parceiros api|worker|migrate|vapid")
	}
	mode := args[0]
	if mode == "vapid" {
		publica, privada, err := notificacoes.GerarChavesVAPID()
		if err != nil {
			return err
		}
		fmt.Printf("VAPID_PUBLICA=%s\nVAPID_PRIVADA=%s\n", publica, privada)
		return nil
	}

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

// canaisNotificacao monta o e-mail (SMTP) e o Web Push, se configurados.
func canaisNotificacao(log *slog.Logger, cfg config.Config) (notificacoes.Remetente, notificacoes.Push) {
	var remetente notificacoes.Remetente
	if cfg.SMTPAddr != "" {
		remetente = notificacoes.SMTP{Addr: cfg.SMTPAddr, Usuario: cfg.SMTPUsuario, Senha: cfg.SMTPSenha, De: cfg.SMTPRemetente}
	} else {
		log.Warn("sem SMTP_ADDR: as notificações não vão por e-mail")
	}
	var push notificacoes.Push
	if cfg.VAPIDPublica != "" {
		push = notificacoes.WebPush{Publica: cfg.VAPIDPublica, Privada: cfg.VAPIDPrivada, Assunto: cfg.VAPIDContato}
	} else {
		log.Warn("sem VAPID_PUBLICA e VAPID_PRIVADA: as notificações não vão por Web Push")
	}
	return remetente, push
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
	remetente, push := canaisNotificacao(log, cfg)
	notificacoesSvc := notificacoes.NewService(pool, notificacoes.FilaRiver{Client: fila}, contasSvc, remetente, push, cfg.AppURL, log)
	contasSvc.EnviarConvitesCom(notificacoesSvc.EnviarConvite)
	midiaSvc := midia.NewService(pool, produtosSvc, &midia.OEmbed{Cache: midia.CacheRedis{R: rdb}},
		objetosMidia(novoBucket(ctx, log, cfg)), nil, contasSvc, &midia.FilaRiver{Client: fila}, log)
	curadoriaSvc := curadoria.NewService(pool, produtosSvc, colecoesSvc, contasSvc, notificacoesSvc, midiaSvc, log)
	afiliador := shopee.Afiliador{Credenciais: credenciais, Cliente: clienteShopee}
	resultadosSvc := resultados.NewService(pool, shopee.Relatorio{Credenciais: credenciais, Cliente: clienteShopee},
		afiliador, produtosSvc, contasSvc, curadoriaSvc, resultados.FilaRiver{Client: fila}, log)
	contas.NewHandler(contasSvc, log).Rotas(router, verificador,
		shopee.NewHandler(credenciais, log).Modulo(),
		tendencias.NewHandler(radar, log).Modulo(),
		colecoes.NewHandler(colecoesSvc, log).Modulo(),
		curadoria.NewHandler(curadoriaSvc, log).Modulo(),
		notificacoes.NewHandler(notificacoesSvc, log).Modulo(),
		midia.NewHandler(midiaSvc, log).Modulo(),
		resultados.NewHandler(resultadosSvc, log).Modulo(),
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
	credenciais := shopee.NovasCredenciais(pool, crypto.NovoCofre(kek), clienteShopee)
	afiliador := shopee.Afiliador{Credenciais: credenciais, Cliente: clienteShopee}
	periodicos = append(periodicos, resultados.PeriodicoSync())

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.PingWorker{Log: log})
	river.AddWorker(workers, &produtos.AgendarSnapshotsWorker{
		Svc: produtosSvc, Fonte: fontes.Shopee, Paginas: cfg.ShopeePaginas,
	})
	bucket := novoBucket(ctx, log, cfg)
	if catalogo != nil {
		var brutos storage.Storage = storage.Descartar{}
		if bucket != nil {
			brutos = bucket
		}
		river.AddWorker(workers, &produtos.SnapshotCatalogoWorker{
			Svc: produtosSvc, Catalogo: catalogo, Storage: brutos, Log: log,
			Depois: tendencias.Enfileirar,
		})
	}
	river.AddWorker(workers, &tendencias.CalcularTendenciasWorker{
		Svc: tendencias.NewService(pool, produtosSvc), Log: log,
	})
	river.AddWorker(workers, &colecoes.GerarLinkWorker{
		Svc: colecoes.NewService(pool, produtosSvc, catalogo, afiliador, nil, log), Log: log,
	})
	remetente, push := canaisNotificacao(log, cfg)
	contasSvc := contas.NewService(pool, nil, cfg.AppURL) // só lê contatos
	river.AddWorker(workers, &notificacoes.EntregarWorker{
		Svc: notificacoes.NewService(pool, nil, contasSvc, remetente, push, cfg.AppURL, log),
	})
	// A revalidação agenda a próxima rodada pela fila, que recebe o cliente
	// logo abaixo, antes de o worker começar.
	filaMidia := &midia.FilaRiver{}
	midiaSvc := midia.NewService(pool, produtosSvc, &midia.OEmbed{}, objetosMidia(bucket), midia.FFmpeg{}, contasSvc, filaMidia, log)
	river.AddWorker(workers, &midia.ProcessarVideoWorker{Svc: midiaSvc, Log: log})
	river.AddWorker(workers, &midia.RevalidarEmbedWorker{Svc: midiaSvc})
	river.AddWorker(workers, &midia.LimparUploadWorker{Svc: midiaSvc})
	river.AddWorker(workers, &resultados.AgendarSyncWorker{Usuarios: credenciais})
	river.AddWorker(workers, &resultados.SyncConversoesWorker{
		Svc: resultados.NewService(pool, shopee.Relatorio{Credenciais: credenciais, Cliente: clienteShopee},
			afiliador, produtosSvc, contasSvc, nil, nil, log),
		Log: log,
	})

	client, err := jobs.NewWorkerClient(pool, workers, periodicos, log)
	if err != nil {
		return err
	}
	filaMidia.Client = client
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

// novoBucket abre o bucket S3 (respostas brutas da Shopee e vídeos). Sem
// S3_ENDPOINT, devolve nil.
func novoBucket(ctx context.Context, log *slog.Logger, cfg config.Config) *storage.S3 {
	if cfg.S3Endpoint == "" {
		log.Warn("sem S3_ENDPOINT: sem bucket para vídeos e respostas brutas")
		return nil
	}
	s3, err := storage.NovoS3(storage.Config{
		Endpoint: cfg.S3Endpoint, EndpointPublico: cfg.S3EndpointPublico, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Region: cfg.S3Region,
	})
	if err != nil {
		log.Error("bucket S3 inválido; sem vídeos nem respostas brutas", "err", err)
		return nil
	}
	if cfg.Env == "dev" {
		if err := s3.GarantirBucket(ctx); err != nil {
			log.Warn("não foi possível criar o bucket local", "err", err)
		}
	}
	return s3
}

// objetosMidia evita passar um *storage.S3 nil como interface não nula.
func objetosMidia(s3 *storage.S3) midia.Objetos {
	if s3 == nil {
		return nil
	}
	return s3
}
