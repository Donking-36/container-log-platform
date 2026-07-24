package ingestion

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPrepareLogBuildsPersistenceModel(t *testing.T) {
	logOffset := int64(123)

	input := EventInput{
		SourceEventID: " event-000001 ",
		AgentID:       " filebeat-agent-1 ",
		ContainerName: " log-producer-1 ",
		ContainerID:   " container-abc ",
		Service:       " log-producer ",
		Level:         " info ",
		Message:       "example log message",
		Source:        " STDOUT ",
		LogPath:       " /logs/application.log ",
		LogOffset:     &logOffset,
		LoggedAt:      "2026-07-24T10:00:00+08:00",
		RawEvent: json.RawMessage(
			`{ "original": true }`,
		),
	}

	utcPlusEight := time.FixedZone(
		"UTC+08:00",
		8*60*60,
	)

	ingestedAt := time.Date(
		2026,
		time.July,
		24,
		10,
		0,
		1,
		0,
		utcPlusEight,
	)

	got, err := PrepareLog(
		input,
		testMaxMessageBytes,
		ingestedAt,
	)
	if err != nil {
		t.Fatalf(
			"PrepareLog() returned an error: %v",
			err,
		)
	}

	if got.ID != 0 {
		t.Fatalf(
			"ID = %d, want 0 before persistence",
			got.ID,
		)
	}

	const wantEventID = "v1:" +
		"5dd33e25f1c52868e9722b639ad6fd50" +
		"54ad6672a551ff055dd0794bc7ae472b"

	if got.EventID != wantEventID {
		t.Fatalf(
			"EventID = %q, want %q",
			got.EventID,
			wantEventID,
		)
	}

	if got.SourceEventID == nil ||
		*got.SourceEventID != "event-000001" {
		t.Fatalf(
			"SourceEventID = %v, want event-000001",
			got.SourceEventID,
		)
	}

	if got.ContainerName != "log-producer-1" {
		t.Fatalf(
			"ContainerName = %q, want log-producer-1",
			got.ContainerName,
		)
	}

	if got.Level != "INFO" {
		t.Fatalf(
			"Level = %q, want INFO",
			got.Level,
		)
	}

	if got.Source != "stdout" {
		t.Fatalf(
			"Source = %q, want stdout",
			got.Source,
		)
	}

	wantLoggedAt := time.Date(
		2026,
		time.July,
		24,
		2,
		0,
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

	if got.IngestedAt.Location() != time.UTC {
		t.Fatalf(
			"IngestedAt location = %v, want UTC",
			got.IngestedAt.Location(),
		)
	}

	wantIngestedAt := time.Date(
		2026,
		time.July,
		24,
		2,
		0,
		1,
		0,
		time.UTC,
	)

	if !got.IngestedAt.Equal(wantIngestedAt) {
		t.Fatalf(
			"IngestedAt = %s, want %s",
			got.IngestedAt,
			wantIngestedAt,
		)
	}

	if got.RawEvent == nil ||
		*got.RawEvent != `{"original":true}` {
		t.Fatalf(
			"RawEvent = %v, want compact JSON",
			got.RawEvent,
		)
	}
	if got.ContainerID == nil ||
		*got.ContainerID != "container-abc" {
		t.Fatalf(
			"ContainerID = %v, want container-abc",
			got.ContainerID,
		)
	}

	if got.Service != "log-producer" {
		t.Fatalf(
			"Service = %q, want log-producer",
			got.Service,
		)
	}

	if got.Message != input.Message {
		t.Fatalf(
			"Message = %q, want %q",
			got.Message,
			input.Message,
		)
	}

	if got.LogPath == nil ||
		*got.LogPath != "/logs/application.log" {
		t.Fatalf(
			"LogPath = %v, want /logs/application.log",
			got.LogPath,
		)
	}

	if got.LogOffset == nil || *got.LogOffset != 123 {
		t.Fatalf(
			"LogOffset = %v, want 123",
			got.LogOffset,
		)
	}
}
func TestPrepareLogKeepsEventIDStableAcrossIngestionTimes(
	t *testing.T,
) {
	input := validEventInput()

	first, err := PrepareLog(
		input,
		testMaxMessageBytes,
		time.Date(
			2026,
			time.July,
			24,
			10,
			0,
			0,
			0,
			time.UTC,
		),
	)
	if err != nil {
		t.Fatalf(
			"first PrepareLog() returned an error: %v",
			err,
		)
	}

	second, err := PrepareLog(
		input,
		testMaxMessageBytes,
		time.Date(
			2026,
			time.July,
			24,
			11,
			0,
			0,
			0,
			time.UTC,
		),
	)
	if err != nil {
		t.Fatalf(
			"second PrepareLog() returned an error: %v",
			err,
		)
	}

	if first.EventID != second.EventID {
		t.Fatalf(
			"event IDs differ: %q and %q",
			first.EventID,
			second.EventID,
		)
	}

	if first.IngestedAt.Equal(second.IngestedAt) {
		t.Fatal(
			"IngestedAt should describe each ingestion time",
		)
	}
}
func TestPrepareLogPreservesValidationError(
	t *testing.T,
) {
	input := validEventInput()
	input.Message = ""

	_, err := PrepareLog(
		input,
		testMaxMessageBytes,
		time.Now(),
	)
	if err == nil {
		t.Fatal("PrepareLog() returned nil error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf(
			"error type = %T, want wrapped *ValidationError",
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
func TestPrepareLogAcceptsFallbackFileIdentity(
	t *testing.T,
) {
	logOffset := int64(0)

	input := EventInput{
		AgentID:       "filebeat-agent-1",
		ContainerName: "log-producer-1",
		Service:       "log-producer",
		Level:         "INFO",
		Message:       "first file log",
		Source:        "file",
		LogPath:       "/logs/application.ndjson",
		LogOffset:     &logOffset,
		LoggedAt:      "2026-07-24T10:00:00Z",
	}

	got, err := PrepareLog(
		input,
		testMaxMessageBytes,
		time.Date(
			2026,
			time.July,
			24,
			10,
			0,
			1,
			0,
			time.UTC,
		),
	)
	if err != nil {
		t.Fatalf(
			"PrepareLog() returned an error: %v",
			err,
		)
	}

	if got.EventID == "" {
		t.Fatal("EventID is empty")
	}

	if got.ContainerID != nil {
		t.Fatalf(
			"ContainerID = %v, want nil",
			got.ContainerID,
		)
	}

	if got.LogOffset == nil || *got.LogOffset != 0 {
		t.Fatalf(
			"LogOffset = %v, want pointer to 0",
			got.LogOffset,
		)
	}
}
func TestNormalizeEventAcceptsMessageAtLimit(
	t *testing.T,
) {
	input := validEventInput()
	input.Message = strings.Repeat(
		"a",
		int(testMaxMessageBytes),
	)

	_, err := NormalizeEvent(
		input,
		testMaxMessageBytes,
	)
	if err != nil {
		t.Fatalf(
			"NormalizeEvent() returned an error: %v",
			err,
		)
	}
}

func TestPrepareLogRejectsZeroIngestedAt(
	t *testing.T,
) {
	_, err := PrepareLog(
		validEventInput(),
		testMaxMessageBytes,
		time.Time{},
	)
	if err == nil {
		t.Fatal(
			"PrepareLog() returned nil error",
		)
	}
}
