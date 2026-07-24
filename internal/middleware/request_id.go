package middleware

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDKey    = "request_id"
	RequestIDHeader = "X-Request-ID"
)

var requestIDPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
)

// RequestID读取上游请求标识，缺失或不合法时生成新标识。
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

// RequestIDFromContext从Gin上下文中读取请求标识。
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
