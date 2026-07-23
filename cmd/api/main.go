package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Donking-36/container-log-platform/internal/config"
	"github.com/Donking-36/container-log-platform/internal/server"
)

const version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	os.Exit(run(logger))
}

func run(logger *slog.Logger) int {
	cfg, err := config.Load()
	if err != nil {
		logger.Error(
			"failed to load application configuration",
			"error", err,
		)
		return 1
	}

	publicRouter := server.NewPublicRouter()
	internalRouter := server.NewInternalRouter()

	httpServers := server.NewServers(
		cfg,
		publicRouter,
		internalRouter,
		logger,
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	logger.Info(
		"container log platform API starting",
		"version", version,
		"app_env", cfg.AppEnv,
		"public_addr", cfg.PublicAddr,
		"internal_addr", cfg.InternalAddr,
	)

	if err := httpServers.Run(ctx); err != nil {
		logger.Error(
			"container log platform API stopped with error",
			"error", err,
		)
		return 1
	}

	logger.Info("container log platform API stopped")

	return 0
}
