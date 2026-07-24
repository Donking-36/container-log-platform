package ingestion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ValidationError 表示事件自身存在永久性字段错误。
// Handler以后可以通过errors.As识别它并返回HTTP 400。
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// NormalizeEvent校验并规范化一条外部日志事件。
func NormalizeEvent(
	input EventInput,
	maxMessageBytes int64,
) (NormalizedEvent, error) {
	if maxMessageBytes <= 0 {
		return NormalizedEvent{}, fmt.Errorf(
			"normalize event: max message bytes must be greater than zero",
		)
	}

	containerName := strings.TrimSpace(input.ContainerName)
	if containerName == "" {
		return NormalizedEvent{}, invalidField(
			"container_name",
			"must not be empty",
		)
	}

	service := strings.TrimSpace(input.Service)
	if service == "" {
		return NormalizedEvent{}, invalidField(
			"service",
			"must not be empty",
		)
	}

	// 正文不能TrimSpace，因为空格可能就是日志的真实内容。
	if input.Message == "" {
		return NormalizedEvent{}, invalidField(
			"message",
			"must not be empty",
		)
	}

	// Go字符串的len返回UTF-8字节数，符合64 KiB的限制语义。
	if int64(len(input.Message)) > maxMessageBytes {
		return NormalizedEvent{}, invalidField(
			"message",
			fmt.Sprintf(
				"must not exceed %d bytes",
				maxMessageBytes,
			),
		)
	}

	source := strings.ToLower(strings.TrimSpace(input.Source))
	switch source {
	case "stdout", "stderr", "file":
	default:
		return NormalizedEvent{}, invalidField(
			"source",
			"must be one of stdout, stderr, file",
		)
	}

	loggedAtText := strings.TrimSpace(input.LoggedAt)
	if loggedAtText == "" {
		return NormalizedEvent{}, invalidField(
			"logged_at",
			"must not be empty",
		)
	}

	loggedAt, err := time.Parse(
		time.RFC3339Nano,
		loggedAtText,
	)
	if err != nil {
		return NormalizedEvent{}, invalidField(
			"logged_at",
			"must use RFC3339 format",
		)
	}

	if input.LogOffset != nil && *input.LogOffset < 0 {
		return NormalizedEvent{}, invalidField(
			"log_offset",
			"must not be negative",
		)
	}

	sourceEventID := optionalString(input.SourceEventID)
	agentID := strings.TrimSpace(input.AgentID)
	containerID := optionalString(input.ContainerID)
	logPath := optionalString(input.LogPath)
	logOffset := optionalInt64(input.LogOffset)

	// 没有来源事件ID时，只能依靠采集位置计算稳定指纹，
	// 因此这些来源字段必须存在。
	if sourceEventID == nil {
		if agentID == "" {
			return NormalizedEvent{}, invalidField(
				"agent_id",
				"must not be empty when source_event_id is absent",
			)
		}

		if logPath == nil {
			return NormalizedEvent{}, invalidField(
				"log_path",
				"must not be empty when source_event_id is absent",
			)
		}

		if logOffset == nil {
			return NormalizedEvent{}, invalidField(
				"log_offset",
				"must be provided when source_event_id is absent",
			)
		}

		if (source == "stdout" || source == "stderr") &&
			containerID == nil {
			return NormalizedEvent{}, invalidField(
				"container_id",
				"must not be empty for container logs without source_event_id",
			)
		}
	}

	rawEvent, err := normalizeRawEvent(input.RawEvent)
	if err != nil {
		return NormalizedEvent{}, err
	}

	return NormalizedEvent{
		SourceEventID: sourceEventID,
		AgentID:       agentID,
		ContainerName: containerName,
		ContainerID:   containerID,
		Service:       service,
		Level:         normalizeLevel(input.Level),
		Message:       input.Message,
		Source:        source,
		LogPath:       logPath,
		LogOffset:     logOffset,
		LoggedAt:      loggedAt.UTC(),
		RawEvent:      rawEvent,
	}, nil
}

func normalizeLevel(value string) string {
	level := strings.ToUpper(strings.TrimSpace(value))

	switch level {
	case "DEBUG", "INFO", "WARN", "ERROR":
		return level
	default:
		return "UNKNOWN"
	}
}

func normalizeRawEvent(
	value json.RawMessage,
) (*string, error) {
	trimmed := bytes.TrimSpace(value)

	if len(trimmed) == 0 ||
		bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, invalidField(
			"raw_event",
			"must contain valid JSON",
		)
	}

	normalized := compact.String()
	return &normalized, nil
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func optionalInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}

	// 创建副本，避免结果继续引用调用者传入的变量。
	copied := *value
	return &copied
}

func invalidField(field string, reason string) error {
	return &ValidationError{
		Field:  field,
		Reason: reason,
	}
}
