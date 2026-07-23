package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/config"
)

func TestServersRunStopsAfterContextCancellation(t *testing.T) {
	cfg := config.Default()
	cfg.PublicAddr = ":0"
	cfg.InternalAddr = "127.0.0.1:0"
	cfg.ShutdownTimeout = time.Second

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	handler := http.NewServeMux()

	servers := NewServers(
		cfg,
		handler,
		handler,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- servers.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}
