package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const eventIDVersion = "v1"

type sourceEventFingerprint struct {
	Version       string `json:"version"`
	Service       string `json:"service"`
	SourceEventID string `json:"source_event_id"`
}

type fallbackFingerprint struct {
	Version     string `json:"version"`
	AgentID     string `json:"agent_id"`
	ContainerID string `json:"container_id"`
	Source      string `json:"source"`
	LogPath     string `json:"log_path"`
	LogOffset   int64  `json:"log_offset"`
	LoggedAt    string `json:"logged_at"`
	Message     string `json:"message"`
}

// ComputeEventID为规范化事件计算稳定的平台幂等键。
func ComputeEventID(
	event NormalizedEvent,
) (string, error) {
	input, err := fingerprintInput(event)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf(
			"compute event ID: encode fingerprint: %w",
			err,
		)
	}

	digest := sha256.Sum256(payload)

	return eventIDVersion +
		":" +
		hex.EncodeToString(digest[:]), nil
}

func fingerprintInput(
	event NormalizedEvent,
) (any, error) {
	if event.SourceEventID != nil {
		if event.Service == "" {
			return nil, fmt.Errorf(
				"compute event ID: service must not be empty",
			)
		}

		if *event.SourceEventID == "" {
			return nil, fmt.Errorf(
				"compute event ID: source_event_id must not be empty",
			)
		}

		return sourceEventFingerprint{
			Version:       eventIDVersion,
			Service:       event.Service,
			SourceEventID: *event.SourceEventID,
		}, nil
	}

	if event.AgentID == "" {
		return nil, fmt.Errorf(
			"compute event ID: agent_id must not be empty",
		)
	}

	if event.LogPath == nil || *event.LogPath == "" {
		return nil, fmt.Errorf(
			"compute event ID: log_path must not be empty",
		)
	}

	if event.LogOffset == nil {
		return nil, fmt.Errorf(
			"compute event ID: log_offset must be provided",
		)
	}

	if *event.LogOffset < 0 {
		return nil, fmt.Errorf(
			"compute event ID: log_offset must not be negative",
		)
	}

	switch event.Source {
	case "stdout", "stderr":
		if event.ContainerID == nil ||
			*event.ContainerID == "" {
			return nil, fmt.Errorf(
				"compute event ID: container_id must " +
					"be provided for container logs",
			)
		}
	case "file":
	default:
		return nil, fmt.Errorf(
			"compute event ID: source must be " +
				"stdout, stderr, or file",
		)
	}

	if event.LoggedAt.IsZero() {
		return nil, fmt.Errorf(
			"compute event ID: logged_at must not be zero",
		)
	}

	if event.Message == "" {
		return nil, fmt.Errorf(
			"compute event ID: message must not be empty",
		)
	}

	containerID := ""
	if event.ContainerID != nil {
		containerID = *event.ContainerID
	}

	return fallbackFingerprint{
		Version:     eventIDVersion,
		AgentID:     event.AgentID,
		ContainerID: containerID,
		Source:      event.Source,
		LogPath:     *event.LogPath,
		LogOffset:   *event.LogOffset,
		LoggedAt: event.LoggedAt.
			UTC().
			Format(time.RFC3339Nano),
		Message: event.Message,
	}, nil
}
