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
	Cycles   int64
}

type lookupEnvFunc func(string) (string, bool)

func loadConfig(
	lookupEnv lookupEnvFunc,
) (config, error) {
	cfg := config{
		LogPath:  defaultProducerLogPath,
		Interval: defaultProducerInterval,
		Cycles:   defaultProducerCycles,
	}

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
