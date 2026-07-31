package model

import "time"

// LogFilter 描述日志读取和统计接口共用的固定过滤条件。
// Start 与 End 均包含边界，并以日志产生时间 LoggedAt 为准。
type LogFilter struct {
	ContainerName string
	Service       string
	Level         string
	Start         *time.Time
	End           *time.Time
}

// LogListQuery 描述 Repository 执行列表查询所需的过滤和分页信息。
type LogListQuery struct {
	// Filter 只包含白名单字段，避免 Repository 接收任意 SQL 条件。
	Filter LogFilter
	// Limit 和 Offset 已由 Service 校验，可直接应用于数据库查询。
	Limit  int
	Offset int
}
