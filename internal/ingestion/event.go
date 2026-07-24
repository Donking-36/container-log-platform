package ingestion

import (
	"encoding/json"
	"time"
)

// EventInput 表示内部HTTP接口收到的原始日志事件。
// 这里的所有内容都来自外部，尚未完成校验。
type EventInput struct {
	SourceEventID string          `json:"source_event_id"`
	AgentID       string          `json:"agent_id"`
	ContainerName string          `json:"container_name"`
	ContainerID   string          `json:"container_id"`
	Service       string          `json:"service"`
	Level         string          `json:"level"`
	Message       string          `json:"message"`
	Source        string          `json:"source"`
	LogPath       string          `json:"log_path"`
	LogOffset     *int64          `json:"log_offset"`
	LoggedAt      string          `json:"logged_at"`
	RawEvent      json.RawMessage `json:"raw_event"`
}

// NormalizedEvent 表示已经校验并规范化、但尚未持久化的事件。
type NormalizedEvent struct {
	SourceEventID *string
	AgentID       string
	ContainerName string
	ContainerID   *string
	Service       string
	Level         string
	Message       string
	Source        string
	LogPath       *string
	LogOffset     *int64
	LoggedAt      time.Time
	RawEvent      *string
}
