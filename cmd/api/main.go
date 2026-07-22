package main

import (
	"log/slog"
	"os"
)

const version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	logger.Info(
		"container log platform API starting",
		"version", version,
	)
}
