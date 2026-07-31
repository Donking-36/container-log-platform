package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultProducerLogPath  = "./data/producer/events.ndjson"
	defaultProducerInterval = time.Second
	defaultProducerCycles   = int64(0)
)

type config struct {
	RunID    string
	LogPath  string
	Interval time.Duration
	// Cycles 为 0 时持续运行；大于 0 时生成指定轮次后正常退出。
	Cycles int64
}

type lookupEnvFunc func(string) (string, bool)

// loadConfig 将环境变量注入抽象为函数，生产环境使用 os.LookupEnv，
// 测试则可传入确定性配置而不修改进程环境。
func loadConfig(
	lookupEnv lookupEnvFunc,
) (config, error) {
	cfg := config{
		LogPath:  defaultProducerLogPath,
		Interval: defaultProducerInterval,
		Cycles:   defaultProducerCycles,
	}

	// 仅当变量未设置时生成随机运行标识；显式设置为空仍是配置错误，
	// 防止不同运行产生无法追踪或无法稳定去重的事件。
	if value, exists := lookupEnv("PRODUCER_RUN_ID"); exists {
		cfg.RunID = strings.TrimSpace(value)
	} else {
		randomID, err := uuid.NewRandom()
		if err != nil {
			return config{}, fmt.Errorf(
				"generate producer run ID: %w",
				err,
			)
		}

		cfg.RunID = "run-" + randomID.String()
	}

	if value, exists := lookupEnv("PRODUCER_LOG_PATH"); exists {
		cfg.LogPath = strings.TrimSpace(value)
	}

	if value, exists := lookupEnv("PRODUCER_INTERVAL"); exists {
		interval, err := time.ParseDuration(
			strings.TrimSpace(value),
		)
		if err != nil {
			return config{}, fmt.Errorf(
				"PRODUCER_INTERVAL must be a valid duration: %w",
				err,
			)
		}

		cfg.Interval = interval
	}

	if value, exists := lookupEnv("PRODUCER_CYCLES"); exists {
		cycles, err := strconv.ParseInt(
			strings.TrimSpace(value),
			10,
			64,
		)
		if err != nil {
			return config{}, fmt.Errorf(
				"PRODUCER_CYCLES must be a valid integer: %w",
				err,
			)
		}

		cfg.Cycles = cycles
	}

	if err := cfg.validate(); err != nil {
		return config{}, fmt.Errorf(
			"validate producer configuration: %w",
			err,
		)
	}

	return cfg, nil
}

func (c config) validate() error {
	if strings.TrimSpace(c.RunID) == "" {
		return fmt.Errorf("PRODUCER_RUN_ID must not be empty")
	}

	if strings.ContainsAny(c.RunID, " \t\r\n") {
		return fmt.Errorf(
			"PRODUCER_RUN_ID must not contain whitespace",
		)
	}

	if strings.TrimSpace(c.LogPath) == "" {
		return fmt.Errorf("PRODUCER_LOG_PATH must not be empty")
	}

	if c.Interval <= 0 {
		return fmt.Errorf(
			"PRODUCER_INTERVAL must be greater than zero",
		)
	}

	if c.Cycles < 0 {
		return fmt.Errorf(
			"PRODUCER_CYCLES must be zero or greater",
		)
	}

	return nil
}
