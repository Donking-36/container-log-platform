package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

// Servers 管理公开和内部两个 HTTP Server。
// 双监听器共享生命周期，但保持不同网络边界和路由集合。
type Servers struct {
	public          *http.Server
	internal        *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
	readiness       *Readiness
}

// NewServers 创建公开和内部 HTTP Server。
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

// Run 启动两个 Server，并等待关闭信号或运行错误。
// 它先绑定两个端口再标记 ready，避免一个端口启动失败时另一个端口短暂
// 对外提供不完整服务；退出时先摘除就绪状态，再并发优雅关闭。
func (s *Servers) Run(ctx context.Context) error {
	// 监听器在启动 goroutine 前同步创建，启动失败可以直接返回给调用方。
	publicListener, err := net.Listen("tcp", s.public.Addr)
	if err != nil {
		return fmt.Errorf(
			"listen on public HTTP address %q: %w",
			s.public.Addr,
			err,
		)
	}

	internalListener, err := net.Listen("tcp", s.internal.Addr)
	if err != nil {
		closeErr := publicListener.Close()

		return errors.Join(
			fmt.Errorf(
				"listen on internal HTTP address %q: %w",
				s.internal.Addr,
				err,
			),
			closeErr,
		)
	}

	// 即使后续流程提前返回，也确保两个监听器被关闭。
	defer func() {
		_ = publicListener.Close()
		_ = internalListener.Close()
	}()

	errCh := make(chan error, 2)

	go s.serve(
		"public",
		s.public,
		publicListener,
		errCh,
	)
	go s.serve(
		"internal",
		s.internal,
		internalListener,
		errCh,
	)

	// 两个端口均成功绑定后，才能标记 ready。
	s.readiness.SetReady(true)
	defer s.readiness.SetReady(false)

	var runErr error

	select {
	case <-ctx.Done():
		s.logger.Info(
			"HTTP server shutdown requested",
			"event", "service_shutdown",
		)
	case runErr = <-errCh:
	}

	s.readiness.SetReady(false)

	shutdownErr := s.shutdown()

	if shutdownErr == nil {
		s.logger.Info(
			"HTTP servers stopped",
			"event", "service_shutdown",
		)
	}

	return errors.Join(runErr, shutdownErr)
}

func (s *Servers) serve(
	name string,
	httpServer *http.Server,
	listener net.Listener,
	errCh chan<- error,
) {
	s.logger.Info(
		"HTTP server listening",
		"event", "service_ready",
		"server", name,
		"address", listener.Addr().String(),
	)

	err := httpServer.Serve(listener)

	if errors.Is(err, http.ErrServerClosed) {
		errCh <- nil
		return
	}

	// 任一 Server 的非正常退出都会唤醒 Run，由 Run 统一关闭另一个 Server，
	// 防止进程只剩半套接口继续提供服务。
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
		// Shutdown 会等待正在处理的请求自然完成，并停止接受新连接。
		// 两个 Server 都成功关闭时无需强制 Close。
		return nil
	}

	// 超时或其他优雅关闭错误后强制释放监听器和剩余连接。
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
