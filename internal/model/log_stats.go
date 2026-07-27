package model

// LogLevelCount表示某个日志级别对应的日志数量。
type LogLevelCount struct {
	Level string
	Count int64
}

// LogServiceCount表示某个服务对应的日志数量。
type LogServiceCount struct {
	Service string
	Count   int64
}
