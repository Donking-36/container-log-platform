package ingestion

import (
	"fmt"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

// PrepareLog 把外部事件转换为可以持久化的日志模型。
// 规范化、幂等键计算和入库时间赋值集中在这里，保证单条与批量接收
// 使用完全一致的转换规则。
func PrepareLog(
	input EventInput,
	maxMessageBytes int64,
	ingestedAt time.Time,
) (model.Log, error) {
	if ingestedAt.IsZero() {
		return model.Log{}, fmt.Errorf(
			"prepare log: ingested_at must not be zero",
		)
	}

	normalized, err := NormalizeEvent(
		input,
		maxMessageBytes,
	)
	if err != nil {
		return model.Log{}, fmt.Errorf(
			"prepare log: normalize event: %w",
			err,
		)
	}

	eventID, err := ComputeEventID(normalized)
	if err != nil {
		return model.Log{}, fmt.Errorf(
			"prepare log: compute event ID: %w",
			err,
		)
	}

	return model.Log{
		EventID: eventID,
		SourceEventID: cloneStringPointer(
			normalized.SourceEventID,
		),
		ContainerName: normalized.ContainerName,
		ContainerID: cloneStringPointer(
			normalized.ContainerID,
		),
		Service: normalized.Service,
		Level:   normalized.Level,
		Message: normalized.Message,
		Source:  normalized.Source,
		LogPath: cloneStringPointer(
			normalized.LogPath,
		),
		LogOffset: optionalInt64(
			normalized.LogOffset,
		),
		LoggedAt:   normalized.LoggedAt.UTC(),
		IngestedAt: ingestedAt.UTC(),
		RawEvent: cloneStringPointer(
			normalized.RawEvent,
		),
	}, nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}

	copied := *value
	return &copied
}
