package config

import (
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	want := Config{
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

	got := Default()

	if got != want {
		t.Fatalf("Default() = %#v, want %#v", got, want)
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("default configuration should be valid: %v", err)
	}
}

func TestConfigValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "empty app environment",
			mutate: func(cfg *Config) {
				cfg.AppEnv = ""
			},
		},
		{
			name: "same listener addresses",
			mutate: func(cfg *Config) {
				cfg.InternalAddr = cfg.PublicAddr
			},
		},
		{
			name: "invalid log level",
			mutate: func(cfg *Config) {
				cfg.LogLevel = "verbose"
			},
		},
		{
			name: "non-positive shutdown timeout",
			mutate: func(cfg *Config) {
				cfg.ShutdownTimeout = 0
			},
		},
		{
			name: "non-positive message limit",
			mutate: func(cfg *Config) {
				cfg.MaxLogMessageBytes = 0
			},
		},
		{
			name: "non-positive ingestion body limit",
			mutate: func(cfg *Config) {
				cfg.MaxIngestBodyBytes = 0
			},
		},
		{
			name: "non-positive ingestion batch size",
			mutate: func(cfg *Config) {
				cfg.MaxIngestBatchSize = 0
			},
		},
		{
			name: "maximum page size below default",
			mutate: func(cfg *Config) {
				cfg.MaxPageSize = cfg.DefaultPageSize - 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)

			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() returned nil for invalid configuration")
			}
		})
	}
}
func TestLoadReadsEnvironment(t *testing.T) {
	setValidEnvironment(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an unexpected error: %v", err)
	}

	want := Config{
		AppEnv:             "test",
		PublicAddr:         ":18080",
		InternalAddr:       ":18081",
		DatabasePath:       "/tmp/log-platform.db",
		LogLevel:           "debug",
		ShutdownTimeout:    3 * time.Second,
		MaxLogMessageBytes: 2048,
		MaxIngestBodyBytes: 4096,
		MaxIngestBatchSize: 500,
		DefaultPageSize:    10,
		MaxPageSize:        50,
	}

	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "invalid duration",
			key:   "SHUTDOWN_TIMEOUT",
			value: "quickly",
		},
		{
			name:  "invalid message size",
			key:   "MAX_LOG_MESSAGE_BYTES",
			value: "large",
		},
		{
			name:  "invalid ingestion body size",
			key:   "MAX_INGEST_BODY_BYTES",
			value: "large",
		},
		{
			name:  "invalid ingestion batch size",
			key:   "MAX_INGEST_BATCH_SIZE",
			value: "many",
		},
		{
			name:  "invalid page size",
			key:   "DEFAULT_PAGE_SIZE",
			value: "many",
		},
		{
			name:  "same listener addresses",
			key:   "INTERNAL_ADDR",
			value: ":18080",
		},
		{
			name:  "empty application environment",
			key:   "APP_ENV",
			value: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(tt.key, tt.value)

			if _, err := Load(); err == nil {
				t.Fatal("Load() returned nil for invalid environment")
			}
		})
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("APP_ENV", "test")
	t.Setenv("PUBLIC_ADDR", ":18080")
	t.Setenv("INTERNAL_ADDR", ":18081")
	t.Setenv("DATABASE_PATH", "/tmp/log-platform.db")
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("MAX_LOG_MESSAGE_BYTES", "2048")
	t.Setenv("MAX_INGEST_BODY_BYTES", "4096")
	t.Setenv("MAX_INGEST_BATCH_SIZE", "500")
	t.Setenv("DEFAULT_PAGE_SIZE", "10")
	t.Setenv("MAX_PAGE_SIZE", "50")
}
