package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/model"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// LogQueryService描述Handler需要的最小日志查询能力。
type LogQueryService interface {
	ListLogs(
		ctx context.Context,
		input service.ListLogsInput,
	) (service.ListLogsResult, error)

	GetLog(
		ctx context.Context,
		id int64,
	) (model.Log, error)
}

// QueryHandler处理公开日志查询HTTP请求。
type QueryHandler struct {
	service LogQueryService
}

// NewQueryHandler创建公开查询Handler。
func NewQueryHandler(
	queryService LogQueryService,
) (*QueryHandler, error) {
	if queryService == nil {
		return nil, errors.New(
			"create query handler: service must not be nil",
		)
	}

	return &QueryHandler{
		service: queryService,
	}, nil
}

// ListLogs处理GET /api/v1/logs。
func (h *QueryHandler) ListLogs(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			"query string is malformed",
		)
		return
	}

	input, err := parseListLogsInput(values)
	if err != nil {
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			err.Error(),
		)
		return
	}

	result, err := h.service.ListLogs(
		c.Request.Context(),
		input,
	)
	if err != nil {
		writeQueryServiceError(c, err)
		return
	}

	data := make([]logListData, 0, len(result.Logs))
	for _, logEntry := range result.Logs {
		data = append(data, newLogListData(logEntry))
	}

	middleware.AddLogAttributes(
		c,
		slog.String("operation", "log_query"),
		slog.Int("result_count", len(data)),
		slog.Int64("total", result.Total),
	)

	c.JSON(http.StatusOK, listLogsResponse{
		Data: data,
		Pagination: paginationData{
			Page:     result.Page,
			PageSize: result.PageSize,
			Total:    result.Total,
		},
		RequestID: middleware.RequestIDFromContext(c),
	})
}

// GetLog处理GET /api/v1/logs/:id。
func (h *QueryHandler) GetLog(c *gin.Context) {
	id, err := parsePositiveLogID(c.Param("id"))
	if err != nil {
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			"id must be a positive integer",
		)
		return
	}

	logEntry, err := h.service.GetLog(
		c.Request.Context(),
		id,
	)
	if err != nil {
		writeQueryServiceError(c, err)
		return
	}

	data, err := newLogDetailData(logEntry)
	if err != nil {
		_ = c.Error(err)
		middleware.WriteError(
			c,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"internal server error",
		)
		return
	}

	middleware.AddLogAttributes(
		c,
		slog.String("operation", "log_detail"),
		slog.Int64("log_id", id),
	)

	c.JSON(http.StatusOK, logDetailResponse{
		Data:      data,
		RequestID: middleware.RequestIDFromContext(c),
	})
}

type paginationData struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type logListData struct {
	ID            int64     `json:"id"`
	EventID       string    `json:"event_id"`
	SourceEventID *string   `json:"source_event_id"`
	ContainerName string    `json:"container_name"`
	ContainerID   *string   `json:"container_id"`
	Service       string    `json:"service"`
	Level         string    `json:"level"`
	Message       string    `json:"message"`
	Source        string    `json:"source"`
	LogPath       *string   `json:"log_path"`
	LogOffset     *int64    `json:"log_offset"`
	LoggedAt      time.Time `json:"logged_at"`
	IngestedAt    time.Time `json:"ingested_at"`
}

type logDetailData struct {
	ID            int64           `json:"id"`
	EventID       string          `json:"event_id"`
	SourceEventID *string         `json:"source_event_id"`
	ContainerName string          `json:"container_name"`
	ContainerID   *string         `json:"container_id"`
	Service       string          `json:"service"`
	Level         string          `json:"level"`
	Message       string          `json:"message"`
	Source        string          `json:"source"`
	LogPath       *string         `json:"log_path"`
	LogOffset     *int64          `json:"log_offset"`
	LoggedAt      time.Time       `json:"logged_at"`
	IngestedAt    time.Time       `json:"ingested_at"`
	RawEvent      json.RawMessage `json:"raw_event"`
}

type listLogsResponse struct {
	Data       []logListData  `json:"data"`
	Pagination paginationData `json:"pagination"`
	RequestID  string         `json:"request_id"`
}

type logDetailResponse struct {
	Data      logDetailData `json:"data"`
	RequestID string        `json:"request_id"`
}

func newLogListData(logEntry model.Log) logListData {
	return logListData{
		ID:            logEntry.ID,
		EventID:       logEntry.EventID,
		SourceEventID: logEntry.SourceEventID,
		ContainerName: logEntry.ContainerName,
		ContainerID:   logEntry.ContainerID,
		Service:       logEntry.Service,
		Level:         logEntry.Level,
		Message:       logEntry.Message,
		Source:        logEntry.Source,
		LogPath:       logEntry.LogPath,
		LogOffset:     logEntry.LogOffset,
		LoggedAt:      logEntry.LoggedAt,
		IngestedAt:    logEntry.IngestedAt,
	}
}

func newLogDetailData(
	logEntry model.Log,
) (logDetailData, error) {
	var rawEvent json.RawMessage

	if logEntry.RawEvent != nil {
		encoded := []byte(*logEntry.RawEvent)
		if !json.Valid(encoded) {
			return logDetailData{}, errors.New(
				"encode log detail: stored raw_event is invalid JSON",
			)
		}

		rawEvent = append(json.RawMessage(nil), encoded...)
	}

	return logDetailData{
		ID:            logEntry.ID,
		EventID:       logEntry.EventID,
		SourceEventID: logEntry.SourceEventID,
		ContainerName: logEntry.ContainerName,
		ContainerID:   logEntry.ContainerID,
		Service:       logEntry.Service,
		Level:         logEntry.Level,
		Message:       logEntry.Message,
		Source:        logEntry.Source,
		LogPath:       logEntry.LogPath,
		LogOffset:     logEntry.LogOffset,
		LoggedAt:      logEntry.LoggedAt,
		IngestedAt:    logEntry.IngestedAt,
		RawEvent:      rawEvent,
	}, nil
}

func parseListLogsInput(
	values url.Values,
) (service.ListLogsInput, error) {
	if err := validateListQueryParameters(values); err != nil {
		return service.ListLogsInput{}, err
	}

	start, err := parseOptionalRFC3339(values, "start")
	if err != nil {
		return service.ListLogsInput{}, err
	}

	end, err := parseOptionalRFC3339(values, "end")
	if err != nil {
		return service.ListLogsInput{}, err
	}

	page, err := parseOptionalPositiveInt(values, "page")
	if err != nil {
		return service.ListLogsInput{}, err
	}

	pageSize, err := parseOptionalPositiveInt(
		values,
		"page_size",
	)
	if err != nil {
		return service.ListLogsInput{}, err
	}

	return service.ListLogsInput{
		ContainerName: values.Get("container"),
		Service:       values.Get("service"),
		Level:         values.Get("level"),
		Start:         start,
		End:           end,
		Page:          page,
		PageSize:      pageSize,
	}, nil
}

func validateListQueryParameters(values url.Values) error {
	allowed := []string{
		"container",
		"service",
		"level",
		"start",
		"end",
		"page",
		"page_size",
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}

	for name := range values {
		if _, exists := allowedSet[name]; !exists {
			return errors.New("unsupported query parameter")
		}
	}

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

func parseOptionalRFC3339(
	values url.Values,
	name string,
) (*time.Time, error) {
	entries, exists := values[name]
	if !exists {
		return nil, nil
	}

	value := strings.TrimSpace(entries[0])
	if value == "" {
		return nil, fmt.Errorf(
			"%s must be a valid RFC3339 timestamp",
			name,
		)
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf(
			"%s must be a valid RFC3339 timestamp",
			name,
		)
	}

	return &parsed, nil
}

func parseOptionalPositiveInt(
	values url.Values,
	name string,
) (*int, error) {
	entries, exists := values[name]
	if !exists {
		return nil, nil
	}

	value, err := strconv.Atoi(
		strings.TrimSpace(entries[0]),
	)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf(
			"%s must be a positive integer",
			name,
		)
	}

	return &value, nil
}

func parsePositiveLogID(value string) (int64, error) {
	id, err := strconv.ParseInt(
		strings.TrimSpace(value),
		10,
		64,
	)
	if err != nil || id <= 0 {
		return 0, service.ErrInvalidLogID
	}

	return id, nil
}

func writeQueryServiceError(
	c *gin.Context,
	err error,
) {
	_ = c.Error(err)

	switch {
	case errors.Is(err, service.ErrInvalidTimeRange):
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_TIME_RANGE",
			"start must not be later than end",
		)
	case errors.Is(err, service.ErrInvalidPagination),
		errors.Is(err, service.ErrInvalidLogID):
		middleware.WriteError(
			c,
			http.StatusBadRequest,
			"INVALID_ARGUMENT",
			"invalid query parameters",
		)
	case errors.Is(err, service.ErrLogNotFound):
		middleware.WriteError(
			c,
			http.StatusNotFound,
			"LOG_NOT_FOUND",
			"log not found",
		)
	case errors.Is(err, service.ErrTemporarilyUnavailable):
		middleware.WriteError(
			c,
			http.StatusServiceUnavailable,
			"SERVICE_UNAVAILABLE",
			"service is temporarily unavailable",
		)
	default:
		middleware.WriteError(
			c,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"internal server error",
		)
	}
}
