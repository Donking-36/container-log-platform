package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunProducerWritesAcceptanceCounts(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	logPath := filepath.Join(
		t.TempDir(),
		"events.ndjson",
	)
	loggedAt := time.Date(
		2026,
		time.July,
		27,
		8,
		0,
		0,
		123456789,
		time.FixedZone("CST", 8*60*60),
	)

	err := runProducer(
		context.Background(),
		config{
			RunID:    "test-run",
			LogPath:  logPath,
			Interval: time.Nanosecond,
			Cycles:   100,
		},
		&stdout,
		&stderr,
		func() time.Time {
			return loggedAt
		},
	)
	if err != nil {
		t.Fatalf("run producer: %v", err)
	}

	stdoutEvents := decodeEvents(
		t,
		stdout.Bytes(),
	)
	stderrEvents := decodeEvents(
		t,
		stderr.Bytes(),
	)

	fileContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read NDJSON log: %v", err)
	}
	fileEvents := decodeEvents(t, fileContent)

	if len(stdoutEvents) != 100 {
		t.Fatalf(
			"stdout events = %d, want 100",
			len(stdoutEvents),
		)
	}

	if len(stderrEvents) != 20 {
		t.Fatalf(
			"stderr events = %d, want 20",
			len(stderrEvents),
		)
	}

	if len(fileEvents) != 100 {
		t.Fatalf(
			"file events = %d, want 100",
			len(fileEvents),
		)
	}

	assertEvent(
		t,
		stdoutEvents[0],
		"test-run-stdout-000001",
		1,
		"INFO",
		"stdout event 000001",
		"2026-07-27T00:00:00.123456789Z",
	)
	assertEvent(
		t,
		stderrEvents[0],
		"test-run-stderr-000001",
		1,
		"ERROR",
		"stderr event 000001",
		"2026-07-27T00:00:00.123456789Z",
	)
	assertEvent(
		t,
		fileEvents[99],
		"test-run-file-000100",
		100,
		"WARN",
		"file event 000100",
		"2026-07-27T00:00:00.123456789Z",
	)

	assertUniqueEventIDs(
		t,
		stdoutEvents,
		stderrEvents,
		fileEvents,
	)
}

func TestRunProducerAppendsFileEvents(t *testing.T) {
	logPath := filepath.Join(
		t.TempDir(),
		"events.ndjson",
	)

	for _, runID := range []string{"first", "second"} {
		err := runProducer(
			context.Background(),
			config{
				RunID:    runID,
				LogPath:  logPath,
				Interval: time.Nanosecond,
				Cycles:   1,
			},
			&bytes.Buffer{},
			&bytes.Buffer{},
			func() time.Time {
				return time.Unix(0, 0)
			},
		)
		if err != nil {
			t.Fatalf(
				"run producer %q: %v",
				runID,
				err,
			)
		}
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read NDJSON log: %v", err)
	}

	events := decodeEvents(t, content)
	if len(events) != 2 {
		t.Fatalf(
			"file events = %d, want 2",
			len(events),
		)
	}

	if events[0].SourceEventID !=
		"first-file-000001" {
		t.Fatalf(
			"first source event ID = %q",
			events[0].SourceEventID,
		)
	}

	if events[1].SourceEventID !=
		"second-file-000001" {
		t.Fatalf(
			"second source event ID = %q",
			events[1].SourceEventID,
		)
	}
}

func decodeEvents(
	t *testing.T,
	content []byte,
) []logEvent {
	t.Helper()

	events := make([]logEvent, 0)
	scanner := bufio.NewScanner(
		bytes.NewReader(content),
	)

	for scanner.Scan() {
		var event logEvent
		if err := json.Unmarshal(
			scanner.Bytes(),
			&event,
		); err != nil {
			t.Fatalf(
				"decode event %q: %v",
				scanner.Text(),
				err,
			)
		}

		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}

	return events
}

func assertEvent(
	t *testing.T,
	actual logEvent,
	wantID string,
	wantSequence int64,
	wantLevel string,
	wantMessage string,
	wantLoggedAt string,
) {
	t.Helper()

	if actual.SourceEventID != wantID ||
		actual.Sequence != wantSequence ||
		actual.Level != wantLevel ||
		actual.Message != wantMessage ||
		actual.LoggedAt != wantLoggedAt {
		t.Fatalf(
			"event = %+v, want id=%q sequence=%d level=%q message=%q logged_at=%q",
			actual,
			wantID,
			wantSequence,
			wantLevel,
			wantMessage,
			wantLoggedAt,
		)
	}
}

func assertUniqueEventIDs(
	t *testing.T,
	groups ...[]logEvent,
) {
	t.Helper()

	seen := make(map[string]struct{})

	for _, events := range groups {
		for _, event := range events {
			if _, exists := seen[event.SourceEventID]; exists {
				t.Fatalf(
					"duplicate source event ID %q",
					event.SourceEventID,
				)
			}

			seen[event.SourceEventID] = struct{}{}
		}
	}

	if len(seen) != 220 {
		t.Fatalf(
			"unique source event IDs = %d, want 220",
			len(seen),
		)
	}
}
