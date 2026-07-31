package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// ErrorDetail 表示统一错误响应中的错误信息。
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse 表示 API 统一错误响应。
type ErrorResponse struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

// WriteError 写入包含 request ID 的统一 JSON 错误响应。
// 同时把稳定的 error_code 附加到请求日志，方便按错误类型聚合排查。
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
