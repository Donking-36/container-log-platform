package main

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLoadConfigUsesDefaults(t *testing.T) {
	cfg, err := loadConfig(
		func(string) (string, bool) {
			return "", false
		},
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	const prefix = "run-"
	if !strings.HasPrefix(cfg.RunID, prefix) {
		t.Fatalf("RunID = %q", cfg.RunID)
	}

	if _, err := uuid.Parse(
		strings.TrimPrefix(cfg.RunID, prefix),
	); err != nil {
		t.Fatalf(
			"RunID %q does not contain a UUID: %v",
			cfg.RunID,
			err,
		)
	}

	if cfg.LogPath != defaultProducerLogPath {
		t.Fatalf("LogPath = %q", cfg.LogPath)
	}

	if cfg.Interval != defaultProducerInterval {
		t.Fatalf("Interval = %s", cfg.Interval)
	}

	if cfg.Cycles != defaultProducerCycles {
		t.Fatalf("Cycles = %d", cfg.Cycles)
	}
}

func TestLoadConfigUsesEnvironment(t *testing.T) {
	values := map[string]string{
		"PRODUCER_RUN_ID":   "acceptance-run",
		"PRODUCER_LOG_PATH": "/logs/events.ndjson",
		"PRODUCER_INTERVAL": "250ms",
		"PRODUCER_CYCLES":   "100",
	}

	cfg, err := loadConfig(
		func(name string) (string, bool) {
			value, exists := values[name]
			return value, exists
		},
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.RunID != "acceptance-run" {
		t.Fatalf("RunID = %q", cfg.RunID)
	}

	if cfg.LogPath != "/logs/events.ndjson" {
		t.Fatalf("LogPath = %q", cfg.LogPath)
	}

	if cfg.Interval != 250*time.Millisecond {
		t.Fatalf("Interval = %s", cfg.Interval)
	}

	if cfg.Cycles != 100 {
		t.Fatalf("Cycles = %d", cfg.Cycles)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		wantMessage string
	}{
		{
			name:        "empty run ID",
			key:         "PRODUCER_RUN_ID",
			value:       " ",
			wantMessage: "PRODUCER_RUN_ID must not be empty",
		},
		{
			name:        "run ID containing whitespace",
			key:         "PRODUCER_RUN_ID",
			value:       "run one",
			wantMessage: "must not contain whitespace",
		},
		{
			name:        "empty log path",
			key:         "PRODUCER_LOG_PATH",
			value:       " ",
			wantMessage: "PRODUCER_LOG_PATH must not be empty",
		},
		{
			name:        "invalid interval",
			key:         "PRODUCER_INTERVAL",
			value:       "soon",
			wantMessage: "must be a valid duration",
		},
		{
			name:        "zero interval",
			key:         "PRODUCER_INTERVAL",
			value:       "0s",
			wantMessage: "must be greater than zero",
		},
		{
			name:        "invalid cycles",
			key:         "PRODUCER_CYCLES",
			value:       "many",
			wantMessage: "must be a valid integer",
		},
		{
			name:        "negative cycles",
			key:         "PRODUCER_CYCLES",
			value:       "-1",
			wantMessage: "must be zero or greater",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(
				func(name string) (string, bool) {
					if name == tt.key {
						return tt.value, true
					}

					return "", false
				},
			)
			if err == nil {
				t.Fatal("load config succeeded")
			}

			if !strings.Contains(
				err.Error(),
				tt.wantMessage,
			) {
				t.Fatalf(
					"error = %q, want substring %q",
					err,
					tt.wantMessage,
				)
			}
		})
	}
}
