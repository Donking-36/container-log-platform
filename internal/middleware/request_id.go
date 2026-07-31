package middleware

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDKey = "request_id"
	// RequestIDHeader 是客户端传入及服务端回传请求标识使用的 HTTP Header。
	RequestIDHeader = "X-Request-ID"
)

// requestIDPattern 限制上游标识的字符集和长度，避免控制字符进入响应头及日志。
var requestIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

// RequestID 读取上游请求标识，缺失或不合法时生成新标识。
// 最终标识同时写入 Gin 上下文和响应头，供后续中间件与客户端关联请求。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(
			c.GetHeader(RequestIDHeader),
		)
		if !requestIDPattern.MatchString(requestID) {
			requestID = uuid.NewString()
		}

		c.Set(requestIDKey, requestID)
		c.Header(RequestIDHeader, requestID)

		c.Next()
	}
}

// RequestIDFromContext 从 Gin 上下文中读取已经校验的请求标识。
func RequestIDFromContext(c *gin.Context) string {
	value, exists := c.Get(requestIDKey)
	if !exists {
		return ""
	}

	requestID, ok := value.(string)
	if !ok {
		return ""
	}

	return requestID
}
