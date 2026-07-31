package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/gin-gonic/gin"
)

type statusResponse struct {
	Status string `json:"status"`
}

// PublicLogHandler 描述公开 Router 需要注册的日志查询 Handler。
type PublicLogHandler interface {
	ListLogs(*gin.Context)
	GetLog(*gin.Context)
}

// PublicStatsHandler 描述公开 Router 需要注册的日志统计 Handler。
type PublicStatsHandler interface {
	GetLevelStats(*gin.Context)
	GetServiceStats(*gin.Context)
}

// InternalLogHandler 描述内部 Router 需要注册的日志接收 Handler。
type InternalLogHandler interface {
	IngestOne(*gin.Context)
	IngestBatch(*gin.Context)
}

// NewPublicRouter 创建对用户公开的 HTTP 路由。
// /healthz 只表示进程存活，/readyz 还会检查接流量开关和数据库；
// 所有业务查询接口均由就绪门禁保护。
func NewPublicRouter(
	logHandler PublicLogHandler,
	statsHandler PublicStatsHandler,
	readiness *Readiness,
	logger *slog.Logger,
) (*gin.Engine, error) {
	if logHandler == nil {
		return nil, errors.New(
			"create public router: log handler must not be nil",
		)
	}

	if statsHandler == nil {
		return nil, errors.New(
			"create public router: stats handler must not be nil",
		)
	}

	if readiness == nil {
		return nil, errors.New(
			"create public router: readiness must not be nil",
		)
	}

	router := newRouter(logger)

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, statusResponse{
			Status: "ok",
		})
	})

	router.GET("/readyz", func(c *gin.Context) {
		if !readiness.Ready(c.Request.Context()) {
			c.JSON(
				http.StatusServiceUnavailable,
				statusResponse{
					Status: "not_ready",
				},
			)
			return
		}

		c.JSON(http.StatusOK, statusResponse{
			Status: "ready",
		})
	})

	readyOnly := router.Group(
		"/api/v1",
		requireReady(readiness),
	)

	readyOnly.GET(
		"/logs",
		logHandler.ListLogs,
	)
	readyOnly.GET(
		"/logs/:id",
		logHandler.GetLog,
	)
	readyOnly.GET(
		"/stats/levels",
		statsHandler.GetLevelStats,
	)
	readyOnly.GET(
		"/stats/services",
		statsHandler.GetServiceStats,
	)

	return router, nil
}

// NewInternalRouter 创建只供 Compose 内部服务访问的 HTTP 路由。
// 该 Router 只注册写入端点，实际网络隔离由 ingestion-net 和未发布的
// 8081 端口共同保证。
func NewInternalRouter(
	logHandler InternalLogHandler,
	readiness *Readiness,
	logger *slog.Logger,
) (*gin.Engine, error) {
	if logHandler == nil {
		return nil, errors.New(
			"create internal router: log handler must not be nil",
		)
	}

	if readiness == nil {
		return nil, errors.New(
			"create internal router: readiness must not be nil",
		)
	}

	router := newRouter(logger)
	readyOnly := router.Group(
		"/internal/v1",
		requireReady(readiness),
	)
	readyOnly.POST(
		"/logs",
		logHandler.IngestOne,
	)
	readyOnly.POST(
		"/logs/bulk",
		logHandler.IngestBatch,
	)

	return router, nil
}

func newRouter(logger *slog.Logger) *gin.Engine {
	// 顺序有意固定：先建立 request ID，再记录完整请求结果，Recovery 位于
	// 最内层捕获 Handler panic。Gin 按注册顺序进入、逆序返回。
	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.RequestLogger(logger),
		middleware.Recovery(logger),
	)

	return router
}

func requireReady(readiness *Readiness) gin.HandlerFunc {
	// 在 shutdown 开始时快速返回 503，避免继续接收新业务请求，
	// 同时保留 /healthz 和 /readyz 用于容器编排探测。
	return func(c *gin.Context) {
		if readiness.Ready(c.Request.Context()) {
			c.Next()
			return
		}

		middleware.WriteError(
			c,
			http.StatusServiceUnavailable,
			"SERVICE_UNAVAILABLE",
			"service is not ready",
		)
		c.Abort()
	}
}
