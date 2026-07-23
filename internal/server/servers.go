package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Donking-36/container-log-platform/internal/config"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

// Servers 管理公开和内部两个HTTP Server。
type Servers struct {
	public          *http.Server
	internal        *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
	readiness       *Readiness
}

// NewServers 创建公开和内部HTTP Server。
func NewServers(
	cfg config.Config,
	publicHandler http.Handler,
	internalHandler http.Handler,
	readiness *Readiness,
	logger *slog.Logger,
) *Servers {
	return &Servers{
		public: newHTTPServer(
			cfg.PublicAddr,
			publicHandler,
		),
		internal: newHTTPServer(
			cfg.InternalAddr,
			internalHandler,
		),
		shutdownTimeout: cfg.ShutdownTimeout,
		readiness:       readiness,
		logger:          logger,
	}
}

func newHTTPServer(
	addr string,
	handler http.Handler,
) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Run 启动两个Server，并等待关闭信号或运行错误。
func (s *Servers) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go s.serve("public", s.public, errCh)
	go s.serve("internal", s.internal, errCh)

	s.readiness.SetReady(true)

	// 防止未来给Run增加提前返回路径时遗忘关闭ready。
	defer s.readiness.SetReady(false)

	var runErr error

	select {
	case <-ctx.Done():
		s.logger.Info("HTTP server shutdown requested")
	case runErr = <-errCh:
	}

	// 必须在停止接收请求之前变为未就绪。
	s.readiness.SetReady(false)

	shutdownErr := s.shutdown()

	if shutdownErr == nil {
		s.logger.Info("HTTP servers stopped")
	}

	return errors.Join(runErr, shutdownErr)
}

func (s *Servers) serve(
	name string,
	httpServer *http.Server,
	errCh chan<- error,
) {
	s.logger.Info(
		"HTTP server listening",
		"server", name,
		"address", httpServer.Addr,
	)

	err := httpServer.ListenAndServe()

	if errors.Is(err, http.ErrServerClosed) {
		errCh <- nil
		return
	}

	errCh <- fmt.Errorf("%s HTTP server failed: %w", name, err)
}

func (s *Servers) shutdown() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		s.shutdownTimeout,
	)
	defer cancel()

	errCh := make(chan error, 2)

	go func() {
		errCh <- s.public.Shutdown(ctx)
	}()

	go func() {
		errCh <- s.internal.Shutdown(ctx)
	}()

	shutdownErr := errors.Join(
		<-errCh,
		<-errCh,
	)
	if shutdownErr == nil {
		return nil
	}

	closeErr := errors.Join(
		s.public.Close(),
		s.internal.Close(),
	)

	return errors.Join(
		fmt.Errorf(
			"gracefully shut down HTTP servers: %w",
			shutdownErr,
		),
		closeErr,
	)
}
