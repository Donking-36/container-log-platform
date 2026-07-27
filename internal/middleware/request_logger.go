package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const logAttributesKey = "request_log_attributes"

// AddLogAttributes为当前请求增加安全的结构化日志字段。
func AddLogAttributes(
	c *gin.Context,
	attributes ...slog.Attr,
) {
	if len(attributes) == 0 {
		return
	}

	existingValue, _ := c.Get(logAttributesKey)
	existing, _ := existingValue.([]slog.Attr)

	combined := make(
		[]slog.Attr,
		0,
		len(existing)+len(attributes),
	)
	combined = append(combined, existing...)
	combined = append(combined, attributes...)

	c.Set(logAttributesKey, combined)
}

// RequestLogger记录HTTP方法、路由、状态码、耗时和request ID。
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(c *gin.Context) {
		startedAt := time.Now()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		status := c.Writer.Status()
		attributes := []slog.Attr{
			slog.String("event", "http_request"),
			slog.String(
				"request_id",
				RequestIDFromContext(c),
			),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String(
				"client_ip",
				remoteClientIP(c.Request.RemoteAddr),
			),
			slog.Int("status", status),
			slog.Int64(
				"latency_ms",
				time.Since(startedAt).Milliseconds(),
			),
		}

		if customValue, exists := c.Get(logAttributesKey); exists {
			if custom, ok := customValue.([]slog.Attr); ok {
				attributes = append(attributes, custom...)
			}
		}

		if len(c.Errors) > 0 {
			attributes = append(
				attributes,
				slog.String("error", c.Errors.String()),
			)
		}

		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		case len(c.Errors) > 0:
			// 永久拒绝事件可以返回200，
			// 但仍应在运行日志中体现为警告。
			level = slog.LevelWarn
		}

		logger.LogAttrs(
			c.Request.Context(),
			level,
			"HTTP request completed",
			attributes...,
		)
	}
}

func remoteClientIP(remoteAddr string) string {
	value := strings.TrimSpace(remoteAddr)

	host, _, err := net.SplitHostPort(value)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}

		return ""
	}

	value = strings.Trim(value, "[]")
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}

	return ""
}
