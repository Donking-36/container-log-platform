package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery捕获Handler panic并返回统一JSON错误响应。
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			panicErr := fmt.Errorf(
				"panic recovered: %v",
				recovered,
			)
			_ = c.Error(panicErr)

			logger.ErrorContext(
				c.Request.Context(),
				"HTTP panic recovered",
				"request_id", RequestIDFromContext(c),
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()),
			)

			if c.Writer.Written() {
				c.Abort()
				return
			}

			c.Abort()
			WriteError(
				c,
				http.StatusInternalServerError,
				"INTERNAL_ERROR",
				"internal server error",
			)
		}()

		c.Next()
	}
}
