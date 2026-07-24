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

// PublicLogHandler描述公开Router需要注册的日志查询Handler。
type PublicLogHandler interface {
	ListLogs(*gin.Context)
	GetLog(*gin.Context)
}

// InternalLogHandler描述内部Router需要注册的日志接收Handler。
type InternalLogHandler interface {
	IngestOne(*gin.Context)
	IngestBatch(*gin.Context)
}

// NewPublicRouter 创建对用户公开的HTTP路由。
func NewPublicRouter(
	logHandler PublicLogHandler,
	readiness *Readiness,
	logger *slog.Logger,
) (*gin.Engine, error) {
	if logHandler == nil {
		return nil, errors.New(
			"create public router: log handler must not be nil",
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
	readyOnly.GET("/logs", logHandler.ListLogs)
	readyOnly.GET("/logs/:id", logHandler.GetLog)

	return router, nil
}

// NewInternalRouter创建只供Compose内部服务访问的HTTP路由。
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
	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.RequestLogger(logger),
		middleware.Recovery(logger),
	)

	return router
}

func requireReady(readiness *Readiness) gin.HandlerFunc {
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
