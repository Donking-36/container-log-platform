package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Donking-36/container-log-platform/internal/config"
	"github.com/Donking-36/container-log-platform/internal/repository"
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

	gormDB, err := repository.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		logger.Error(
			"failed to open SQLite database",
			"error", err,
		)
		return 1
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		logger.Error(
			"failed to access SQLite connection pool",
			"error", err,
		)
		return 1
	}

	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error(
				"failed to close SQLite database",
				"error", err,
			)
		}
	}()

	if err := repository.Migrate(gormDB); err != nil {
		logger.Error(
			"failed to migrate SQLite database",
			"error", err,
		)
		return 1
	}

	readiness := server.NewReadiness(sqlDB.PingContext)

	publicRouter := server.NewPublicRouter(readiness)
	internalRouter := server.NewInternalRouter()

	httpServers := server.NewServers(
		cfg,
		publicRouter,
		internalRouter,
		readiness,
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
