package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type statusResponse struct {
	Status string `json:"status"`
}

// NewPublicRouter 创建对用户公开的HTTP路由。
func NewPublicRouter(readiness *Readiness) *gin.Engine {
	router := newRouter()

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

	return router
}

// NewInternalRouter 创建只供Compose内部服务访问的HTTP路由。
func NewInternalRouter() *gin.Engine {
	return newRouter()
}

func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	return router
}
