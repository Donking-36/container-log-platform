package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// LogStatsService 描述 StatsHandler 需要的最小统计能力。
type LogStatsService interface {
	GetLevelStats(
		ctx context.Context,
		input service.StatsInput,
	) (service.LevelStatsResult, error)

	GetServiceStats(
		ctx context.Context,
		input service.StatsInput,
	) (service.ServiceStatsResult, error)
}

// StatsHandler 处理公开日志统计请求。
type StatsHandler struct {
	stats LogStatsService
}

// NewStatsHandler 创建日志统计 Handler。
func NewStatsHandler(
	statsService LogStatsService,
) (*StatsHandler, error) {
	if statsService == nil {
		return nil, errors.New(
			"create stats handler: service must not be nil",
		)
	}

	return &StatsHandler{
		stats: statsService,
	}, nil
}

// GetLevelStats 处理 GET /api/v1/stats/levels。
func (h *StatsHandler) GetLevelStats(c *gin.Context) {
	input, err := parseStatsRequest(
		c.Request.URL.RawQuery,
	)
	if err != nil {
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			err.Error(),
		)
		return
	}

	result, err := h.stats.GetLevelStats(
		c.Request.Context(),
		input,
	)
	if err != nil {
		writeQueryServiceError(c, err)
		return
	}

	data := make(
		[]levelStatData,
		0,
		len(result.Stats),
	)
	for _, item := range result.Stats {
		data = append(data, levelStatData{
			Level:      item.Level,
			Count:      item.Count,
			Percentage: item.Percentage,
		})
	}

	middleware.AddLogAttributes(
		c,
		slog.String(
			"operation",
			"level_statistics",
		),
		slog.Int("result_count", len(data)),
		slog.Int64("total", result.Total),
	)

	c.JSON(http.StatusOK, levelStatsResponse{
		Data:      data,
		Total:     result.Total,
		RequestID: middleware.RequestIDFromContext(c),
	})
}

// GetServiceStats 处理 GET /api/v1/stats/services。
func (h *StatsHandler) GetServiceStats(c *gin.Context) {
	input, err := parseStatsRequest(
		c.Request.URL.RawQuery,
	)
	if err != nil {
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			err.Error(),
		)
		return
	}

	result, err := h.stats.GetServiceStats(
		c.Request.Context(),
		input,
	)
	if err != nil {
		writeQueryServiceError(c, err)
		return
	}

	data := make(
		[]serviceStatData,
		0,
		len(result.Stats),
	)
	for _, item := range result.Stats {
		data = append(data, serviceStatData{
			Service: item.Service,
			Count:   item.Count,
		})
	}

	middleware.AddLogAttributes(
		c,
		slog.String(
			"operation",
			"service_statistics",
		),
		slog.Int("result_count", len(data)),
		slog.Int64("total", result.Total),
	)

	c.JSON(http.StatusOK, serviceStatsResponse{
		Data:      data,
		Total:     result.Total,
		RequestID: middleware.RequestIDFromContext(c),
	})
}

type levelStatData struct {
	Level      string  `json:"level"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

type serviceStatData struct {
	Service string `json:"service"`
	Count   int64  `json:"count"`
}

type levelStatsResponse struct {
	Data      []levelStatData `json:"data"`
	Total     int64           `json:"total"`
	RequestID string          `json:"request_id"`
}

type serviceStatsResponse struct {
	Data      []serviceStatData `json:"data"`
	Total     int64             `json:"total"`
	RequestID string            `json:"request_id"`
}

func parseStatsRequest(
	rawQuery string,
) (service.StatsInput, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return service.StatsInput{}, errors.New(
			"query string is malformed",
		)
	}

	return parseStatsInput(values)
}

func parseStatsInput(
	values url.Values,
) (service.StatsInput, error) {
	if err := validateStatsQueryParameters(values); err != nil {
		return service.StatsInput{}, err
	}

	// 直接复用 Query Handler 已有的 RFC3339 解析函数，
	// 确保列表与统计接口接受完全相同的时间格式。
	start, err := parseOptionalRFC3339(values, "start")
	if err != nil {
		return service.StatsInput{}, err
	}

	end, err := parseOptionalRFC3339(values, "end")
	if err != nil {
		return service.StatsInput{}, err
	}

	return service.StatsInput{
		ContainerName: values.Get("container"),
		Service:       values.Get("service"),
		Level:         values.Get("level"),
		Start:         start,
		End:           end,
	}, nil
}

func validateStatsQueryParameters(
	values url.Values,
) error {
	allowed := []string{
		"container",
		"service",
		"level",
		"start",
		"end",
	}

	allowedSet := make(
		map[string]struct{},
		len(allowed),
	)
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}

	// 拒绝统计接口不支持的参数，例如 page 和 page_size。
	for name := range values {
		if _, exists := allowedSet[name]; !exists {
			return errors.New(
				"unsupported query parameter",
			)
		}
	}

	// 同一个参数只能提供一次。
	for _, name := range allowed {
		entries, exists := values[name]
		if !exists {
			continue
		}

		if len(entries) != 1 {
			return fmt.Errorf(
				"%s must be provided exactly once",
				name,
			)
		}
	}

	return nil
}
