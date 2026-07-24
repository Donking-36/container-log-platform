package model

import "time"

// LogFilter描述日志读取接口支持的固定过滤条件。
type LogFilter struct {
	ContainerName string
	Service       string
	Level         string
	Start         *time.Time
	End           *time.Time
}

// LogListQuery描述Repository执行列表查询所需的过滤和分页信息。
type LogListQuery struct {
	Filter LogFilter
	Limit  int
	Offset int
}
