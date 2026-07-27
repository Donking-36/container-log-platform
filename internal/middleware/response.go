package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// ErrorDetail表示统一错误响应中的错误信息。
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse表示API统一错误响应。
type ErrorResponse struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

// WriteError写入包含request ID的统一JSON错误响应。
func WriteError(
	c *gin.Context,
	status int,
	code string,
	message string,
) {
	AddLogAttributes(
		c,
		slog.String("error_code", code),
	)

	c.JSON(status, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
		RequestID: RequestIDFromContext(c),
	})
}
