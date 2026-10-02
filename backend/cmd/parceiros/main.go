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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/db"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/config"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/jobs"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
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
		return runWorker(ctx, log, pool)
	default:
		return fmt.Errorf("modo desconhecido %q", mode)
	}
}

func runAPI(ctx context.Context, log *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()

	router := httpserver.NewRouter(log, map[string]httpserver.Checker{
		"postgres": pool.Ping,
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	})

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

func runWorker(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool) error {
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.PingWorker{Log: log})

	client, err := jobs.NewWorkerClient(pool, workers, log)
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
