package model

import "time"

// Log 表示持久化到 SQLite 的一条规范化日志。
type Log struct {
	// ID 是 SQLite 生成的内部主键，只用于平台内查询和稳定排序。
	ID int64 `gorm:"primaryKey;autoIncrement;index:idx_logs_container_level_logged_at_id,priority:4;index:idx_logs_service_logged_at_id,priority:3;index:idx_logs_logged_at_id,priority:2"`

	// EventID 是平台生成的全局幂等键；SourceEventID 是来源系统的可选标识。
	EventID       string  `gorm:"type:text;not null;uniqueIndex:uidx_logs_event_id"`
	SourceEventID *string `gorm:"type:text"`

	// ContainerName、ContainerID 和 Service 描述日志归属，并支撑常用过滤索引。
	ContainerName string  `gorm:"type:text;not null;index:idx_logs_container_level_logged_at_id,priority:1"`
	ContainerID   *string `gorm:"type:text"`
	Service       string  `gorm:"type:text;not null;index:idx_logs_service_logged_at_id,priority:1"`

	// Level 与 Message 是规范化后的日志内容，Source 取 stdout、stderr 或 file。
	Level   string `gorm:"type:text;not null;index:idx_logs_container_level_logged_at_id,priority:2"`
	Message string `gorm:"type:text;not null"`
	Source  string `gorm:"type:text;not null"`

	// LogPath 和 LogOffset 标记采集位置，可用于追踪及后备幂等指纹。
	LogPath   *string `gorm:"type:text"`
	LogOffset *int64

	// LoggedAt 是日志产生时间，IngestedAt 是平台接收时间；二者均以 UTC 保存。
	LoggedAt   time.Time `gorm:"not null;index:idx_logs_container_level_logged_at_id,priority:3;index:idx_logs_service_logged_at_id,priority:2;index:idx_logs_logged_at_id,priority:1"`
	IngestedAt time.Time `gorm:"not null"`

	// RawEvent 保留规范化前的 Filebeat JSON，仅详情接口返回以控制列表开销。
	RawEvent *string `gorm:"type:text"`
}

// TableName 固定数据库表名，避免表名受 GORM 命名规则变化影响。
func (Log) TableName() string {
	return "logs"
}
