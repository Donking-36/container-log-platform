package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery 捕获 Handler panic，记录 request ID 与堆栈，并尽可能返回统一
// JSON 错误响应。即使业务代码没有主动调用 panic，运行时错误、第三方库或
// 未来代码仍可能触发 panic，因此它是 HTTP 进程的最后一道隔离边界。
//
// 若响应头已经写出，则不能安全改写状态码，只终止剩余 Handler。
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
