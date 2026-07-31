package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// eventIDVersion 是指纹算法版本。修改字段或编码规则时必须升级版本，
// 避免新旧算法意外生成同一命名空间下的事件标识。
const eventIDVersion = "v1"

// sourceEventFingerprint 用来源系统已经保证稳定的事件标识生成最小指纹。
type sourceEventFingerprint struct {
	Version       string `json:"version"`
	Service       string `json:"service"`
	SourceEventID string `json:"source_event_id"`
}

// fallbackFingerprint 在来源没有事件标识时，使用采集位置和内容生成可重放的指纹。
// 字段顺序由结构体固定，确保 JSON 编码结果稳定。
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

// ComputeEventID 为规范化事件计算稳定的平台幂等键。
// 返回值采用“算法版本:SHA-256”格式；相同来源事件在重放后仍得到相同结果，
// Repository 因而可以依靠 event_id 唯一索引实现幂等写入。
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
		// 来源标识必须与服务名组合，避免不同服务恰好使用相同的本地序号
		// 时被错误地视为同一条日志。
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

	// 没有来源事件标识时，采集器、文件位置、偏移量和日志内容共同构成
	// 后备指纹。Filebeat 重放同一位置时这些字段保持不变，因此仍能去重。
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
