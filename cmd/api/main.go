package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Donking-36/container-log-platform/internal/config"
	"github.com/Donking-36/container-log-platform/internal/handler"
	"github.com/Donking-36/container-log-platform/internal/repository"
	"github.com/Donking-36/container-log-platform/internal/server"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// version 默认标记本地开发构建，发布镜像通过 -ldflags 在编译时注入版本号。
var version = "dev"

func main() {
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	).With("service", "api")

	os.Exit(run(logger))
}

// run 是 API 进程的组合根：按 Config → Repository → Service → Handler →
// Router → Server 的依赖方向完成装配。返回退出码而不是在中途调用 os.Exit，
// 使数据库关闭等 defer 在进程退出前得到执行。
func run(logger *slog.Logger) int {
	// 关闭 Gin 自带的非结构化调试输出，
	// 运行日志统一交给 slog 中间件。
	gin.SetMode(gin.ReleaseMode)

	cfg, err := config.Load()
	if err != nil {
		logger.Error(
			"failed to load application configuration",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	gormDB, err := repository.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		logger.Error(
			"failed to open SQLite database",
			"event", "database_error",
			"error", err,
		)
		return 1
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		logger.Error(
			"failed to access SQLite connection pool",
			"event", "database_error",
			"error", err,
		)
		return 1
	}

	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error(
				"failed to close SQLite database",
				"event", "database_error",
				"error", err,
			)
		}
	}()

	if err := repository.Migrate(gormDB); err != nil {
		logger.Error(
			"failed to migrate SQLite database",
			"event", "database_error",
			"error", err,
		)
		return 1
	}

	logRepository, err := repository.NewLogRepository(gormDB)
	if err != nil {
		logger.Error(
			"failed to create log repository",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	logIngestionService, err := service.NewIngestionService(
		logRepository,
		cfg.MaxLogMessageBytes,
	)
	if err != nil {
		logger.Error(
			"failed to create log ingestion service",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	logIngestionHandler, err := handler.NewIngestionHandler(
		logIngestionService,
		cfg.MaxIngestBodyBytes,
		cfg.MaxIngestBatchSize,
	)
	if err != nil {
		logger.Error(
			"failed to create log ingestion handler",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	logQueryService, err := service.NewQueryService(
		logRepository,
		cfg.DefaultPageSize,
		cfg.MaxPageSize,
	)
	if err != nil {
		logger.Error(
			"failed to create log query service",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	logQueryHandler, err := handler.NewQueryHandler(
		logQueryService,
	)
	if err != nil {
		logger.Error(
			"failed to create log query handler",
			"event", "service_start",
			"error", err,
		)
		return 1
	}
	logStatsService, err := service.NewStatsService(
		logRepository,
	)
	if err != nil {
		logger.Error(
			"failed to create log statistics service",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	logStatsHandler, err := handler.NewStatsHandler(
		logStatsService,
	)
	if err != nil {
		logger.Error(
			"failed to create log statistics handler",
			"event", "service_start",
			"error", err,
		)
		return 1
	}
	readiness := server.NewReadiness(sqlDB.PingContext)

	publicRouter, err := server.NewPublicRouter(
		logQueryHandler,
		logStatsHandler,
		readiness,
		logger,
	)
	if err != nil {
		logger.Error(
			"failed to create public router",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	internalRouter, err := server.NewInternalRouter(
		logIngestionHandler,
		readiness,
		logger,
	)
	if err != nil {
		logger.Error(
			"failed to create internal router",
			"event", "service_start",
			"error", err,
		)
		return 1
	}

	httpServers := server.NewServers(
		cfg,
		publicRouter,
		internalRouter,
		readiness,
		logger,
	)

	// SIGINT 支持本地 Ctrl+C，SIGTERM 支持 Docker stop；
	// 两者统一取消上下文并进入 Servers 的优雅关闭流程。
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	logger.Info(
		"container log platform API starting",
		"event", "service_start",
		"version", version,
		"app_env", cfg.AppEnv,
		"public_addr", cfg.PublicAddr,
		"internal_addr", cfg.InternalAddr,
	)

	if err := httpServers.Run(ctx); err != nil {
		logger.Error(
			"container log platform API stopped with error",
			"event", "service_shutdown",
			"error", err,
		)
		return 1
	}

	logger.Info(
		"container log platform API stopped",
		"event", "service_shutdown",
	)

	return 0
}
