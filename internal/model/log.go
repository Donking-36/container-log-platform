package model

import "time"

// Log 表示持久化到 SQLite 的一条规范化日志。
type Log struct {
	ID int64 `gorm:"primaryKey;autoIncrement;index:idx_logs_container_level_logged_at_id,priority:4;index:idx_logs_service_logged_at_id,priority:3;index:idx_logs_logged_at_id,priority:2"`

	EventID       string  `gorm:"type:text;not null;uniqueIndex:uidx_logs_event_id"`
	SourceEventID *string `gorm:"type:text"`

	ContainerName string  `gorm:"type:text;not null;index:idx_logs_container_level_logged_at_id,priority:1"`
	ContainerID   *string `gorm:"type:text"`
	Service       string  `gorm:"type:text;not null;index:idx_logs_service_logged_at_id,priority:1"`

	Level   string `gorm:"type:text;not null;index:idx_logs_container_level_logged_at_id,priority:2"`
	Message string `gorm:"type:text;not null"`
	Source  string `gorm:"type:text;not null"`

	LogPath   *string `gorm:"type:text"`
	LogOffset *int64

	LoggedAt   time.Time `gorm:"not null;index:idx_logs_container_level_logged_at_id,priority:3;index:idx_logs_service_logged_at_id,priority:2;index:idx_logs_logged_at_id,priority:1"`
	IngestedAt time.Time `gorm:"not null"`

	RawEvent *string `gorm:"type:text"`
}

// TableName 固定数据库表名，避免表名受GORM命名规则变化影响。
func (Log) TableName() string {
	return "logs"
}
