package ingestion

import (
	"testing"
	"time"
)

func TestComputeEventIDWithSourceEventIDGolden(
	t *testing.T,
) {
	sourceEventID := "event-000001"

	event := NormalizedEvent{
		SourceEventID: &sourceEventID,
		Service:       "log-producer",
	}

	got, err := ComputeEventID(event)
	if err != nil {
		t.Fatalf(
			"ComputeEventID() returned an error: %v",
			err,
		)
	}

	const want = "v1:" +
		"5dd33e25f1c52868e9722b639ad6fd50" +
		"54ad6672a551ff055dd0794bc7ae472b"

	if got != want {
		t.Fatalf(
			"event ID = %q, want %q",
			got,
			want,
		)
	}
}

func TestComputeEventIDWithFallbackIdentityGolden(
	t *testing.T,
) {
	containerID := "container-abc"
	logPath := "/var/lib/docker/containers/" +
		"container-abc/container-abc-json.log"
	logOffset := int64(12345)

	utcPlusEight := time.FixedZone(
		"UTC+08:00",
		8*60*60,
	)

	event := NormalizedEvent{
		AgentID:     "filebeat-agent-1",
		ContainerID: &containerID,
		Source:      "stdout",
		LogPath:     &logPath,
		LogOffset:   &logOffset,
		LoggedAt: time.Date(
			2026,
			time.July,
			24,
			10,
			0,
			0,
			123456789,
			utcPlusEight,
		),
		Message: "example log message",
	}

	got, err := ComputeEventID(event)
	if err != nil {
		t.Fatalf(
			"ComputeEventID() returned an error: %v",
			err,
		)
	}

	const want = "v1:" +
		"55dd5af6dd79f46fb69bf96985353670" +
		"60c227719283378c49fe0c46ea27499d"

	if got != want {
		t.Fatalf(
			"event ID = %q, want %q",
			got,
			want,
		)
	}
}

func TestComputeEventIDIsStable(t *testing.T) {
	event := newFallbackEventForIDTest()

	want, err := ComputeEventID(event)
	if err != nil {
		t.Fatalf(
			"first ComputeEventID() returned an error: %v",
			err,
		)
	}

	for attempt := 1; attempt <= 100; attempt++ {
		got, err := ComputeEventID(event)
		if err != nil {
			t.Fatalf(
				"attempt %d returned an error: %v",
				attempt,
				err,
			)
		}

		if got != want {
			t.Fatalf(
				"attempt %d event ID = %q, want %q",
				attempt,
				got,
				want,
			)
		}
	}
}
func TestComputeEventIDDistinguishesSourceNamespace(
	t *testing.T,
) {
	baseline := newSourceEventForIDTest()

	baselineID, err := ComputeEventID(baseline)
	if err != nil {
		t.Fatalf(
			"baseline ComputeEventID() error: %v",
			err,
		)
	}

	tests := []struct {
		name   string
		change func(*NormalizedEvent)
	}{
		{
			name: "different service",
			change: func(event *NormalizedEvent) {
				event.Service = "payment-service"
			},
		},
		{
			name: "different source event ID",
			change: func(event *NormalizedEvent) {
				sourceEventID := "event-000002"
				event.SourceEventID = &sourceEventID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := newSourceEventForIDTest()
			tt.change(&candidate)

			got, err := ComputeEventID(candidate)
			if err != nil {
				t.Fatalf(
					"ComputeEventID() returned an error: %v",
					err,
				)
			}

			if got == baselineID {
				t.Fatalf(
					"changed event ID = %q, want a different ID",
					got,
				)
			}
		})
	}
}
func TestComputeEventIDKeepsSourceEventStableAcrossContainerRestart(
	t *testing.T,
) {
	first := newSourceEventForIDTest()
	second := newSourceEventForIDTest()

	firstContainerID := "container-old"
	secondContainerID := "container-new"

	first.ContainerID = &firstContainerID
	second.ContainerID = &secondContainerID

	firstID, err := ComputeEventID(first)
	if err != nil {
		t.Fatalf(
			"first ComputeEventID() error: %v",
			err,
		)
	}

	secondID, err := ComputeEventID(second)
	if err != nil {
		t.Fatalf(
			"second ComputeEventID() error: %v",
			err,
		)
	}

	if firstID != secondID {
		t.Fatalf(
			"IDs differ after container restart: %q and %q",
			firstID,
			secondID,
		)
	}
}
func TestComputeEventIDDistinguishesFallbackIdentityFields(
	t *testing.T,
) {
	baseline := newFallbackEventForIDTest()

	baselineID, err := ComputeEventID(baseline)
	if err != nil {
		t.Fatalf(
			"baseline ComputeEventID() error: %v",
			err,
		)
	}

	tests := []struct {
		name   string
		change func(*NormalizedEvent)
	}{
		{
			name: "different agent ID",
			change: func(event *NormalizedEvent) {
				event.AgentID = "filebeat-agent-2"
			},
		},
		{
			name: "different container ID",
			change: func(event *NormalizedEvent) {
				containerID := "container-2"
				event.ContainerID = &containerID
			},
		},
		{
			name: "different source",
			change: func(event *NormalizedEvent) {
				event.Source = "stderr"
			},
		},
		{
			name: "different log path",
			change: func(event *NormalizedEvent) {
				logPath := "/logs/other.log"
				event.LogPath = &logPath
			},
		},
		{
			name: "different log offset",
			change: func(event *NormalizedEvent) {
				logOffset := int64(101)
				event.LogOffset = &logOffset
			},
		},
		{
			name: "different logged at",
			change: func(event *NormalizedEvent) {
				event.LoggedAt = event.LoggedAt.Add(
					time.Nanosecond,
				)
			},
		},
		{
			name: "different message",
			change: func(event *NormalizedEvent) {
				event.Message = "different message"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := newFallbackEventForIDTest()
			tt.change(&candidate)

			got, err := ComputeEventID(candidate)
			if err != nil {
				t.Fatalf(
					"ComputeEventID() returned an error: %v",
					err,
				)
			}

			if got == baselineID {
				t.Fatalf(
					"changed event ID = %q, want a different ID",
					got,
				)
			}
		})
	}
}
func TestComputeEventIDRejectsIncompleteFallbackIdentity(
	t *testing.T,
) {
	_, err := ComputeEventID(NormalizedEvent{})
	if err == nil {
		t.Fatal(
			"ComputeEventID() returned nil error",
		)
	}
}
func newSourceEventForIDTest() NormalizedEvent {
	sourceEventID := "event-000001"

	return NormalizedEvent{
		SourceEventID: &sourceEventID,
		Service:       "log-producer",
	}
}

func newFallbackEventForIDTest() NormalizedEvent {
	containerID := "container-1"
	logPath := "/logs/application.log"
	logOffset := int64(100)

	return NormalizedEvent{
		AgentID:       "filebeat-agent-1",
		ContainerName: "log-producer-1",
		ContainerID:   &containerID,
		Service:       "log-producer",
		Level:         "INFO",
		Message:       "example message",
		Source:        "stdout",
		LogPath:       &logPath,
		LogOffset:     &logOffset,
		LoggedAt: time.Date(
			2026,
			time.July,
			24,
			10,
			0,
			0,
			0,
			time.UTC,
		),
	}
}
