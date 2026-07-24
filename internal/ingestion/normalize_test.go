package ingestion

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testMaxMessageBytes int64 = 64 * 1024

func TestNormalizeEventNormalizesValidInput(t *testing.T) {
	input := EventInput{
		SourceEventID: " producer-event-0001 ",
		AgentID:       " filebeat-agent-1 ",
		ContainerName: " log-producer-1 ",
		ContainerID:   " container-123 ",
		Service:       " log-producer ",
		Level:         " info ",
		Message:       "  message spaces must be preserved  ",
		Source:        " STDOUT ",
		LogPath:       " /var/lib/docker/containers/example.log ",
		LoggedAt:      "2026-07-23T18:30:00+08:00",
	}

	got, err := NormalizeEvent(
		input,
		testMaxMessageBytes,
	)
	if err != nil {
		t.Fatalf("NormalizeEvent() returned an error: %v", err)
	}

	if got.SourceEventID == nil ||
		*got.SourceEventID != "producer-event-0001" {
		t.Fatalf(
			"SourceEventID = %v, want producer-event-0001",
			got.SourceEventID,
		)
	}

	if got.ContainerName != "log-producer-1" {
		t.Fatalf(
			"ContainerName = %q, want %q",
			got.ContainerName,
			"log-producer-1",
		)
	}

	if got.Service != "log-producer" {
		t.Fatalf(
			"Service = %q, want %q",
			got.Service,
			"log-producer",
		)
	}

	if got.Level != "INFO" {
		t.Fatalf("Level = %q, want INFO", got.Level)
	}

	if got.Source != "stdout" {
		t.Fatalf("Source = %q, want stdout", got.Source)
	}

	// 日志正文不能TrimSpace，否则可能改变真实日志内容和event_id。
	if got.Message != input.Message {
		t.Fatalf(
			"Message = %q, want original message %q",
			got.Message,
			input.Message,
		)
	}

	wantLoggedAt := time.Date(
		2026,
		time.July,
		23,
		10,
		30,
		0,
		0,
		time.UTC,
	)

	if !got.LoggedAt.Equal(wantLoggedAt) {
		t.Fatalf(
			"LoggedAt = %s, want %s",
			got.LoggedAt,
			wantLoggedAt,
		)
	}
	if got.LoggedAt.Location() != time.UTC {
		t.Fatalf(
			"LoggedAt location = %v, want UTC",
			got.LoggedAt.Location(),
		)
	}

	_, offset := got.LoggedAt.Zone()
	if offset != 0 {
		t.Fatalf(
			"LoggedAt zone offset = %d, want 0",
			offset,
		)
	}
}

func TestNormalizeEventRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*EventInput)
		wantField string
	}{
		{
			name: "missing container name",
			change: func(input *EventInput) {
				input.ContainerName = " "
			},
			wantField: "container_name",
		},
		{
			name: "missing service",
			change: func(input *EventInput) {
				input.Service = ""
			},
			wantField: "service",
		},
		{
			name: "missing message",
			change: func(input *EventInput) {
				input.Message = ""
			},
			wantField: "message",
		},
		{
			name: "message exceeds size limit",
			change: func(input *EventInput) {
				input.Message = strings.Repeat(
					"a",
					int(testMaxMessageBytes)+1,
				)
			},
			wantField: "message",
		},
		{
			name: "missing source",
			change: func(input *EventInput) {
				input.Source = ""
			},
			wantField: "source",
		},
		{
			name: "unsupported source",
			change: func(input *EventInput) {
				input.Source = "console"
			},
			wantField: "source",
		},
		{
			name: "missing logged at",
			change: func(input *EventInput) {
				input.LoggedAt = ""
			},
			wantField: "logged_at",
		},
		{
			name: "invalid logged at",
			change: func(input *EventInput) {
				input.LoggedAt = "not-a-time"
			},
			wantField: "logged_at",
		},
		{
			name: "negative log offset",
			change: func(input *EventInput) {
				input.LogOffset = int64Pointer(-1)
			},
			wantField: "log_offset",
		},
		{
			name: "missing fallback agent ID",
			change: func(input *EventInput) {
				input.SourceEventID = ""
			},
			wantField: "agent_id",
		},
		{
			name: "missing fallback log path",
			change: func(input *EventInput) {
				input.SourceEventID = ""
				input.AgentID = "filebeat-agent-1"
				input.ContainerID = "container-1"
				input.LogOffset = int64Pointer(0)
			},
			wantField: "log_path",
		},
		{
			name: "missing fallback log offset",
			change: func(input *EventInput) {
				input.SourceEventID = ""
				input.AgentID = "filebeat-agent-1"
				input.ContainerID = "container-1"
				input.LogPath = "/var/log/container.log"
			},
			wantField: "log_offset",
		},
		{
			name: "missing fallback container ID",
			change: func(input *EventInput) {
				input.SourceEventID = ""
				input.AgentID = "filebeat-agent-1"
				input.LogPath = "/var/log/container.log"
				input.LogOffset = int64Pointer(0)
			},
			wantField: "container_id",
		},
		{
			name: "comma fractional separator",
			change: func(input *EventInput) {
				input.LoggedAt =
					"2026-07-24T10:00:00,123Z"
			},
			wantField: "logged_at",
		},
		{
			name: "timezone hour out of range",
			change: func(input *EventInput) {
				input.LoggedAt =
					"2026-07-24T10:00:00+24:00"
			},
			wantField: "logged_at",
		},
		{
			name: "timezone minute out of range",
			change: func(input *EventInput) {
				input.LoggedAt =
					"2026-07-24T10:00:00+00:60"
			},
			wantField: "logged_at",
		},
		{
			name: "invalid raw event JSON",
			change: func(input *EventInput) {
				input.RawEvent = []byte("{")
			},
			wantField: "raw_event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 每个子测试都从一份确定合法的输入开始，
			// 然后只破坏当前测试关心的字段。
			input := validEventInput()
			tt.change(&input)

			_, err := NormalizeEvent(
				input,
				testMaxMessageBytes,
			)
			if err == nil {
				t.Fatal(
					"NormalizeEvent() returned nil error",
				)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf(
					"error type = %T, want *ValidationError",
					err,
				)
			}

			if validationErr.Field != tt.wantField {
				t.Fatalf(
					"error field = %q, want %q",
					validationErr.Field,
					tt.wantField,
				)
			}
		})
	}
}
func TestNormalizeEventNormalizesLevel(t *testing.T) {
	tests := []struct {
		name       string
		inputLevel string
		wantLevel  string
	}{
		{
			name:       "lowercase known level",
			inputLevel: " warn ",
			wantLevel:  "WARN",
		},
		{
			name:       "unknown level",
			inputLevel: "verbose",
			wantLevel:  "UNKNOWN",
		},
		{
			name:       "missing level",
			inputLevel: "",
			wantLevel:  "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validEventInput()
			input.Level = tt.inputLevel

			got, err := NormalizeEvent(
				input,
				testMaxMessageBytes,
			)
			if err != nil {
				t.Fatalf(
					"NormalizeEvent() returned an error: %v",
					err,
				)
			}

			if got.Level != tt.wantLevel {
				t.Fatalf(
					"Level = %q, want %q",
					got.Level,
					tt.wantLevel,
				)
			}
		})
	}
}
func TestNormalizeEventAcceptsFallbackFileIdentity(
	t *testing.T,
) {
	input := validEventInput()

	input.SourceEventID = ""
	input.AgentID = " filebeat-agent-1 "
	input.ContainerID = ""
	input.Source = " FILE "
	input.LogPath = " /logs/application.ndjson "
	input.LogOffset = int64Pointer(0)

	got, err := NormalizeEvent(
		input,
		testMaxMessageBytes,
	)
	if err != nil {
		t.Fatalf(
			"NormalizeEvent() returned an error: %v",
			err,
		)
	}

	if got.SourceEventID != nil {
		t.Fatalf(
			"SourceEventID = %v, want nil",
			got.SourceEventID,
		)
	}

	if got.AgentID != "filebeat-agent-1" {
		t.Fatalf(
			"AgentID = %q, want %q",
			got.AgentID,
			"filebeat-agent-1",
		)
	}

	if got.Source != "file" {
		t.Fatalf(
			"Source = %q, want file",
			got.Source,
		)
	}

	if got.LogOffset == nil || *got.LogOffset != 0 {
		t.Fatalf(
			"LogOffset = %v, want pointer to 0",
			got.LogOffset,
		)
	}
}
func TestNormalizeEventCountsMessageSizeInBytes(
	t *testing.T,
) {
	input := validEventInput()

	// 三个汉字在UTF-8中占9字节。
	input.Message = "你好呀"

	_, err := NormalizeEvent(input, 8)
	if err == nil {
		t.Fatal(
			"NormalizeEvent() returned nil error",
		)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf(
			"error type = %T, want *ValidationError",
			err,
		)
	}

	if validationErr.Field != "message" {
		t.Fatalf(
			"error field = %q, want message",
			validationErr.Field,
		)
	}
}
func validEventInput() EventInput {
	return EventInput{
		SourceEventID: "event-001",
		ContainerName: "log-producer-1",
		Service:       "log-producer",
		Level:         "INFO",
		Message:       "test message",
		Source:        "stdout",
		LoggedAt:      "2026-07-24T10:00:00Z",
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
