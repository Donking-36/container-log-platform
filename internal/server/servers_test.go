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

func TestServersRunManagesReadinessAndStopsAfterCancellation(
	t *testing.T,
) {
	cfg := config.Default()
	cfg.PublicAddr = ":0"
	cfg.InternalAddr = "127.0.0.1:0"
	cfg.ShutdownTimeout = time.Second

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	handler := http.NewServeMux()

	readiness := NewReadiness(
		func(context.Context) error {
			return nil
		},
	)

	servers := NewServers(
		cfg,
		handler,
		handler,
		readiness,
		logger,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- servers.Run(ctx)
	}()

	waitUntilReady(t, readiness)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}

	if readiness.Ready(context.Background()) {
		t.Fatal("readiness remained true after Run() stopped")
	}
}

func waitUntilReady(
	t *testing.T,
	readiness *Readiness,
) {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()

	for {
		if readiness.Ready(context.Background()) {
			return
		}

		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("server did not become ready")
		}
	}
}
