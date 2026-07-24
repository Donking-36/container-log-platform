package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 保存API进程运行所需的全部配置。
type Config struct {
	AppEnv             string
	PublicAddr         string
	InternalAddr       string
	DatabasePath       string
	LogLevel           string
	ShutdownTimeout    time.Duration
	MaxLogMessageBytes int64
	MaxIngestBodyBytes int64
	MaxIngestBatchSize int
	DefaultPageSize    int
	MaxPageSize        int
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
