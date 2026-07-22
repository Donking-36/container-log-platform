package main

import (
	"log/slog"
	"os"

	"github.com/Donking-36/container-log-platform/internal/config"
)

const version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error(
			"failed to load application configuration",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info(
		"container log platform API starting",
		"version", version,
		"app_env", cfg.AppEnv,
		"public_addr", cfg.PublicAddr,
		"internal_addr", cfg.InternalAddr,
	)
}
