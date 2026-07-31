package ingestion

import (
	"encoding/json"
	"time"
)

// EventInput 表示内部 HTTP 接口收到的原始日志事件。
// 这里的所有内容都来自外部，尚未完成校验。
type EventInput struct {
	// SourceEventID 是来源系统提供的稳定事件标识；存在时会优先用于幂等判定。
	SourceEventID string `json:"source_event_id"`
	// AgentID 标识采集该事件的 Filebeat 实例。
	AgentID string `json:"agent_id"`
	// ContainerName 是便于查询和展示的容器名称，ContainerID 是容器的稳定标识。
	ContainerName string `json:"container_name"`
	ContainerID   string `json:"container_id"`
	// Service 标识产生日志的逻辑服务。
	Service string `json:"service"`
	// Level、Message 和 Source 描述日志内容及其 stdout、stderr 或 file 来源。
	Level   string `json:"level"`
	Message string `json:"message"`
	Source  string `json:"source"`
	// LogPath 与 LogOffset 描述采集位置，可在缺少 SourceEventID 时参与幂等指纹计算。
	LogPath   string `json:"log_path"`
	LogOffset *int64 `json:"log_offset"`
	// LoggedAt 是来源记录的 RFC3339 时间，RawEvent 保留 Filebeat 原始事件用于追溯。
	LoggedAt string          `json:"logged_at"`
	RawEvent json.RawMessage `json:"raw_event"`
}

// NormalizedEvent 表示已经校验并规范化、但尚未持久化的事件。
// 可选字符串统一使用 nil 表示“来源未提供”，避免空字符串产生歧义。
type NormalizedEvent struct {
	// SourceEventID 与 AgentID 提供两种幂等指纹路径所需的来源身份。
	SourceEventID *string
	AgentID       string
	// ContainerName、ContainerID 和 Service 标识日志归属。
	ContainerName string
	ContainerID   *string
	Service       string
	// Level、Message 和 Source 是完成大小写及枚举规范化后的日志内容。
	Level   string
	Message string
	Source  string
	// LogPath 与 LogOffset 保留采集位置；没有来源事件标识时二者必须存在。
	LogPath   *string
	LogOffset *int64
	// LoggedAt 始终为 UTC，RawEvent 保存压缩后的合法 JSON。
	LoggedAt time.Time
	RawEvent *string
}
