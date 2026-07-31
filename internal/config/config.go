package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 保存 API 进程运行所需的全部配置。
type Config struct {
	// AppEnv 是写入启动日志的运行环境标签。
	AppEnv string
	// PublicAddr 对外提供查询、统计和健康检查接口。
	PublicAddr string
	// InternalAddr 只供 Compose 采集链路写入日志。
	InternalAddr string
	// DatabasePath 是 SQLite 数据库文件路径。
	DatabasePath string
	// LogLevel 是启动时加载并校验的日志级别配置。
	LogLevel string
	// ShutdownTimeout 是双 HTTP Server 优雅关闭的最长等待时间。
	ShutdownTimeout time.Duration
	// MaxLogMessageBytes 限制单条日志正文的 UTF-8 字节数。
	MaxLogMessageBytes int64
	// MaxIngestBodyBytes 限制单次接收请求体的字节数。
	MaxIngestBodyBytes int64
	// MaxIngestBatchSize 限制批量接收接口的一次事件数。
	MaxIngestBatchSize int
	// DefaultPageSize 是未提供 page_size 时使用的默认值。
	DefaultPageSize int
	// MaxPageSize 是客户端可请求的最大每页记录数。
	MaxPageSize int
}

// Default 返回适合本地开发的默认配置。
func Default() Config {
	return Config{
		AppEnv:             "development",
		PublicAddr:         ":8080",
		InternalAddr:       ":8081",
		DatabasePath:       "./data/log-platform.db",
		LogLevel:           "info",
		ShutdownTimeout:    10 * time.Second,
		MaxLogMessageBytes: 64 * 1024,
		MaxIngestBodyBytes: 16 * 1024 * 1024,
		MaxIngestBatchSize: 1000,
		DefaultPageSize:    20,
		MaxPageSize:        100,
	}
}

// Load 从环境变量读取配置，并使用默认值补充未设置的项目。
func Load() (Config, error) {
	cfg := Default()

	cfg.AppEnv = stringFromEnv("APP_ENV", cfg.AppEnv)
	cfg.PublicAddr = stringFromEnv("PUBLIC_ADDR", cfg.PublicAddr)
	cfg.InternalAddr = stringFromEnv("INTERNAL_ADDR", cfg.InternalAddr)
	cfg.DatabasePath = stringFromEnv("DATABASE_PATH", cfg.DatabasePath)
	cfg.LogLevel = strings.ToLower(
		stringFromEnv("LOG_LEVEL", cfg.LogLevel),
	)

	// 各解析函数会区分“环境变量未设置”和“设置了非法值”：
	// 前者使用默认值，后者立即使启动失败，避免悄悄带错配置运行。
	var err error

	cfg.ShutdownTimeout, err = durationFromEnv(
		"SHUTDOWN_TIMEOUT",
		cfg.ShutdownTimeout,
	)
	if err != nil {
		return Config{}, err
	}

	cfg.MaxLogMessageBytes, err = int64FromEnv(
		"MAX_LOG_MESSAGE_BYTES",
		cfg.MaxLogMessageBytes,
	)
	if err != nil {
		return Config{}, err
	}

	cfg.MaxIngestBodyBytes, err = int64FromEnv(
		"MAX_INGEST_BODY_BYTES",
		cfg.MaxIngestBodyBytes,
	)
	if err != nil {
		return Config{}, err
	}

	cfg.MaxIngestBatchSize, err = intFromEnv(
		"MAX_INGEST_BATCH_SIZE",
		cfg.MaxIngestBatchSize,
	)
	if err != nil {
		return Config{}, err
	}

	cfg.DefaultPageSize, err = intFromEnv(
		"DEFAULT_PAGE_SIZE",
		cfg.DefaultPageSize,
	)
	if err != nil {
		return Config{}, err
	}

	cfg.MaxPageSize, err = intFromEnv(
		"MAX_PAGE_SIZE",
		cfg.MaxPageSize,
	)
	if err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}

	return cfg, nil
}

// Validate 在程序启动前检查配置是否合法。
func (c Config) Validate() error {
	if strings.TrimSpace(c.AppEnv) == "" {
		return fmt.Errorf("APP_ENV must not be empty")
	}

	publicAddr := strings.TrimSpace(c.PublicAddr)
	internalAddr := strings.TrimSpace(c.InternalAddr)

	if publicAddr == "" {
		return fmt.Errorf("PUBLIC_ADDR must not be empty")
	}

	if internalAddr == "" {
		return fmt.Errorf("INTERNAL_ADDR must not be empty")
	}

	if publicAddr == internalAddr {
		return fmt.Errorf("PUBLIC_ADDR and INTERNAL_ADDR must be different")
	}

	if strings.TrimSpace(c.DatabasePath) == "" {
		return fmt.Errorf("DATABASE_PATH must not be empty")
	}

	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf(
			"LOG_LEVEL must be one of debug, info, warn, error",
		)
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	if c.MaxLogMessageBytes <= 0 {
		return fmt.Errorf("MAX_LOG_MESSAGE_BYTES must be greater than zero")
	}

	if c.MaxIngestBodyBytes <= 0 {
		return fmt.Errorf(
			"MAX_INGEST_BODY_BYTES must be greater than zero",
		)
	}

	if c.MaxIngestBatchSize <= 0 {
		return fmt.Errorf(
			"MAX_INGEST_BATCH_SIZE must be greater than zero",
		)
	}

	if c.DefaultPageSize <= 0 {
		return fmt.Errorf("DEFAULT_PAGE_SIZE must be greater than zero")
	}

	if c.MaxPageSize <= 0 {
		return fmt.Errorf("MAX_PAGE_SIZE must be greater than zero")
	}

	if c.MaxPageSize < c.DefaultPageSize {
		return fmt.Errorf(
			"MAX_PAGE_SIZE must be greater than or equal to DEFAULT_PAGE_SIZE",
		)
	}

	return nil
}

func stringFromEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}

	return strings.TrimSpace(value)
}

func durationFromEnv(
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a valid duration: %w",
			key,
			err,
		)
	}

	return parsed, nil
}

func int64FromEnv(key string, fallback int64) (int64, error) {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}

	parsed, err := strconv.ParseInt(
		strings.TrimSpace(value),
		10,
		64,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a valid integer: %w",
			key,
			err,
		)
	}

	return parsed, nil
}

func intFromEnv(key string, fallback int) (int, error) {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a valid integer: %w",
			key,
			err,
		)
	}

	return parsed, nil
}
