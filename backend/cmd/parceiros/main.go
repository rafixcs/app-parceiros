// Command parceiros is the single backend binary. The first argument picks
// the mode:
//
//	parceiros api      serves the HTTP API
//	parceiros worker   processes the River jobs
//	parceiros migrate  applies the migrations (goose + River) and exits
//	parceiros vapid    prints a VAPID key pair for Web Push and exits
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/internal/observability"
	"github.com/rafixcs/app-parceiros/backend/internal/server"
)

func main() {
	log := observability.NewLogger(os.Stdout)
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("exiting with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: parceiros api|worker|migrate|vapid")
	}
	mode := args[0]
	if mode == "vapid" {
		public, private, err := notificacoes.GerarChavesVAPID()
		if err != nil {
			return err
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", public, private)
		return nil
	}

	cfg, err := server.LoadConfig()
	if err != nil {
		return err
	}
	log = log.With("mode", mode, "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch mode {
	case "migrate":
		return server.Migrate(ctx, log, cfg)
	case "api":
		return server.RunAPI(ctx, log, cfg)
	case "worker":
		return server.RunWorker(ctx, log, cfg)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}
