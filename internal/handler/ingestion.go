package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/Donking-36/container-log-platform/internal/ingestion"
	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

// LogIngestionService 描述 IngestionHandler 需要的最小日志接收能力。
type LogIngestionService interface {
	IngestOne(
		ctx context.Context,
		input ingestion.EventInput,
	) (service.IngestResult, error)

	IngestBatch(
		ctx context.Context,
		inputs []ingestion.EventInput,
	) (service.IngestResult, error)
}

// IngestionHandler 处理只对采集链路开放的日志接收 HTTP 请求。
type IngestionHandler struct {
	service             LogIngestionService
	maxRequestBodyBytes int64
	maxBatchEvents      int
}

// NewIngestionHandler 创建日志接收 Handler。
func NewIngestionHandler(
	ingestionService LogIngestionService,
	maxRequestBodyBytes int64,
	maxBatchEvents int,
) (*IngestionHandler, error) {
	if ingestionService == nil {
		return nil, errors.New(
			"create ingestion handler: service must not be nil",
		)
	}

	if maxRequestBodyBytes <= 0 {
		return nil, errors.New(
			"create ingestion handler: " +
				"max request body bytes must be greater than zero",
		)
	}

	if maxBatchEvents <= 0 {
		return nil, errors.New(
			"create ingestion handler: " +
				"max batch events must be greater than zero",
		)
	}

	return &IngestionHandler{
		service:             ingestionService,
		maxRequestBodyBytes: maxRequestBodyBytes,
		maxBatchEvents:      maxBatchEvents,
	}, nil
}

// IngestOne 处理 POST /internal/v1/logs。
// 首次写入返回 201，重复事件和永久非法事件返回 200，临时存储故障返回
// 503。这样 Logstash 只会重试真正可能恢复的请求。
func (h *IngestionHandler) IngestOne(c *gin.Context) {
	markIngestionRequest(c)

	var input ingestion.EventInput

	err := decodeJSONBody(
		c,
		&input,
		h.maxRequestBodyBytes,
	)
	if err != nil {
		writeJSONDecodeError(c, err)
		return
	}

	result, err := h.service.IngestOne(
		c.Request.Context(),
		input,
	)
	if err != nil {
		if errors.Is(
			err,
			service.ErrTemporarilyUnavailable,
		) {
			_ = c.Error(err)
			middleware.WriteError(
				c,
				http.StatusServiceUnavailable,
				"SERVICE_UNAVAILABLE",
				"service is temporarily unavailable",
			)
			return
		}

		var validationErr *ingestion.ValidationError

		if errors.As(err, &validationErr) {
			// 事件已经被成功分类为永久拒绝。
			// 返回 200 可避免下游无限重试同一坏事件。
			_ = c.Error(validationErr)

			result := service.IngestResult{
				Received: 1,
				Rejected: 1,
			}
			addIngestionLogAttributes(c, result)

			writeIngestionResponse(
				c,
				http.StatusOK,
				result,
			)
			return
		}

		_ = c.Error(err)
		middleware.WriteError(
			c,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"internal server error",
		)
		return
	}

	status := http.StatusCreated
	if result.Inserted == 0 {
		// 重复提交已经被正确处理，但没有产生新记录。
		status = http.StatusOK
	}

	addIngestionLogAttributes(c, result)
	writeIngestionResponse(c, status, result)
}

// IngestBatch 处理 POST /internal/v1/logs/bulk。
// 批次中的永久非法项会计入 rejected，其他合法项仍可成功入库，因此业务上
// 已完成分类的部分拒绝仍返回 200，避免下游反复重放整个批次。
func (h *IngestionHandler) IngestBatch(c *gin.Context) {
	markIngestionRequest(c)

	var inputs []ingestion.EventInput

	err := decodeJSONBody(
		c,
		&inputs,
		h.maxRequestBodyBytes,
	)
	if err != nil {
		writeJSONDecodeError(c, err)
		return
	}

	if len(inputs) > h.maxBatchEvents {
		middleware.WriteError(
			c,
			http.StatusRequestEntityTooLarge,
			"PAYLOAD_TOO_LARGE",
			fmt.Sprintf(
				"batch must not contain more than %d events",
				h.maxBatchEvents,
			),
		)
		return
	}

	result, err := h.service.IngestBatch(
		c.Request.Context(),
		inputs,
	)
	if err != nil {
		if errors.Is(err, service.ErrEmptyBatch) {
			_ = c.Error(err)
			middleware.WriteError(
				c,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
				"events must not be empty",
			)
			return
		}

		if errors.Is(
			err,
			service.ErrTemporarilyUnavailable,
		) {
			_ = c.Error(err)
			middleware.WriteError(
				c,
				http.StatusServiceUnavailable,
				"SERVICE_UNAVAILABLE",
				"service is temporarily unavailable",
			)
			return
		}

		var validationErr *ingestion.ValidationError

		if errors.As(err, &validationErr) {
			_ = c.Error(validationErr)
			middleware.WriteError(
				c,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
				validationErr.Error(),
			)
			return
		}

		_ = c.Error(err)
		middleware.WriteError(
			c,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"internal server error",
		)
		return
	}

	// 重复和永久拒绝项都已经完成分类，不应触发整批重试。
	addIngestionLogAttributes(c, result)
	if result.Rejected > 0 {
		_ = c.Error(fmt.Errorf(
			"ingestion rejected %d event(s)",
			result.Rejected,
		))
	}

	writeIngestionResponse(
		c,
		http.StatusOK,
		result,
	)
}

// decodeJSONBody 对所有接收接口实施相同的协议边界：限制请求体大小、
// 拒绝未知字段，并确保请求体只包含一个 JSON 值。严格解码可以尽早发现
// Filebeat/Logstash 与 API 的字段契约漂移。
func decodeJSONBody(
	c *gin.Context,
	destination any,
	maxBytes int64,
) error {
	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		maxBytes,
	)

	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}

	// 请求体只能包含一个 JSON 值，拒绝尾随的第二个值。
	var extra any

	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}

	if err == nil {
		return errors.New(
			"decode request body: body must contain one JSON value",
		)
	}

	return fmt.Errorf(
		"decode trailing request body: %w",
		err,
	)
}

// writeJSONDecodeError 将 MaxBytesReader 的超限错误与普通 JSON 语法错误
// 映射为稳定的 HTTP 状态码和错误码。
func writeJSONDecodeError(
	c *gin.Context,
	err error,
) {
	var maxBytesErr *http.MaxBytesError

	if errors.As(err, &maxBytesErr) {
		_ = c.Error(err)
		middleware.WriteError(
			c,
			http.StatusRequestEntityTooLarge,
			"PAYLOAD_TOO_LARGE",
			"request body exceeds configured limit",
		)
		return
	}

	_ = c.Error(err)
	middleware.WriteError(
		c,
		http.StatusBadRequest,
		"INVALID_ARGUMENT",
		"request body must contain one valid JSON value",
	)
}

type ingestionResultData struct {
	Received   int64 `json:"received"`
	Inserted   int64 `json:"inserted"`
	Duplicated int64 `json:"duplicated"`
	Rejected   int64 `json:"rejected"`
}

type ingestionResponse struct {
	Data      ingestionResultData `json:"data"`
	RequestID string              `json:"request_id"`
}

func writeIngestionResponse(
	c *gin.Context,
	status int,
	result service.IngestResult,
) {
	c.JSON(status, ingestionResponse{
		Data: ingestionResultData{
			Received:   result.Received,
			Inserted:   result.Inserted,
			Duplicated: result.Duplicated,
			Rejected:   result.Rejected,
		},
		RequestID: middleware.RequestIDFromContext(c),
	})
}

func addIngestionLogAttributes(
	c *gin.Context,
	result service.IngestResult,
) {
	middleware.AddLogAttributes(
		c,
		slog.Int64("received", result.Received),
		slog.Int64("inserted", result.Inserted),
		slog.Int64("duplicated", result.Duplicated),
		slog.Int64("rejected", result.Rejected),
	)
}

func markIngestionRequest(c *gin.Context) {
	middleware.AddLogAttributes(
		c,
		slog.String("operation", "log_ingestion"),
	)
}
