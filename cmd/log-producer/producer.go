package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const stderrEveryCycles = int64(5)

type logEvent struct {
	SourceEventID string `json:"source_event_id"`
	Sequence      int64  `json:"sequence"`
	Level         string `json:"level"`
	Message       string `json:"message"`
	LoggedAt      string `json:"logged_at"`
}

// runProducer 生成采集链路的确定性日志夹具：每轮写一条 stdout/INFO 和
// 一条文件/WARN 日志，每五轮额外写一条 stderr/ERROR 日志。这个三路比例
// 是端到端验收计数的一部分，修改时必须同步更新测试与验收标准。
func runProducer(
	ctx context.Context,
	cfg config,
	stdout io.Writer,
	stderr io.Writer,
	now func() time.Time,
) error {
	if err := cfg.validate(); err != nil {
		return fmt.Errorf(
			"run producer: invalid configuration: %w",
			err,
		)
	}

	if stdout == nil {
		return fmt.Errorf(
			"run producer: stdout must not be nil",
		)
	}

	if stderr == nil {
		return fmt.Errorf(
			"run producer: stderr must not be nil",
		)
	}

	if now == nil {
		return fmt.Errorf(
			"run producer: clock must not be nil",
		)
	}

	if err := os.MkdirAll(
		filepath.Dir(cfg.LogPath),
		0o755,
	); err != nil {
		return fmt.Errorf(
			"run producer: create log directory: %w",
			err,
		)
	}

	file, err := os.OpenFile(
		cfg.LogPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return fmt.Errorf(
			"run producer: open NDJSON log file: %w",
			err,
		)
	}
	defer file.Close()

	stdoutEncoder := json.NewEncoder(stdout)
	stderrEncoder := json.NewEncoder(stderr)
	fileEncoder := json.NewEncoder(file)

	for sequence := int64(1); ; sequence++ {
		// 每轮开始前先检查取消，保证收到停止信号后不再产生新事件，
		// 只执行已有文件数据的落盘。
		select {
		case <-ctx.Done():
			return syncLogFile(file)
		default:
		}

		loggedAt := now().UTC()

		if err := encodeEvent(
			stdoutEncoder,
			"stdout",
			newLogEvent(
				cfg.RunID,
				"stdout",
				"INFO",
				sequence,
				loggedAt,
			),
		); err != nil {
			return err
		}

		if sequence%stderrEveryCycles == 0 {
			// stderr 使用独立连续序号 1、2、3……，而不是总轮次 5、10、15……，
			// 使每个输出通道的 source_event_id 都连续且便于验收。
			stderrSequence := sequence /
				stderrEveryCycles

			if err := encodeEvent(
				stderrEncoder,
				"stderr",
				newLogEvent(
					cfg.RunID,
					"stderr",
					"ERROR",
					stderrSequence,
					loggedAt,
				),
			); err != nil {
				return err
			}
		}

		if err := encodeEvent(
			fileEncoder,
			"file",
			newLogEvent(
				cfg.RunID,
				"file",
				"WARN",
				sequence,
				loggedAt,
			),
		); err != nil {
			return err
		}

		if cfg.Cycles > 0 && sequence >= cfg.Cycles {
			return syncLogFile(file)
		}

		// 使用 Timer 而不是 Sleep，使 SIGTERM 可以立即打断等待。
		timer := time.NewTimer(cfg.Interval)
		select {
		case <-ctx.Done():
			// Stop 返回 false 表示计时器已经触发；排空通道后再退出，
			// 避免遗留未消费的计时事件。
			if !timer.Stop() {
				<-timer.C
			}
			return syncLogFile(file)
		case <-timer.C:
		}
	}
}

func newLogEvent(
	runID string,
	source string,
	level string,
	sequence int64,
	loggedAt time.Time,
) logEvent {
	return logEvent{
		SourceEventID: fmt.Sprintf(
			"%s-%s-%06d",
			runID,
			source,
			sequence,
		),
		Sequence: sequence,
		Level:    level,
		Message: fmt.Sprintf(
			"%s event %06d",
			source,
			sequence,
		),
		LoggedAt: loggedAt.UTC().Format(
			time.RFC3339Nano,
		),
	}
}

func encodeEvent(
	encoder *json.Encoder,
	destination string,
	event logEvent,
) error {
	if err := encoder.Encode(event); err != nil {
		return fmt.Errorf(
			"run producer: write %s event: %w",
			destination,
			err,
		)
	}

	return nil
}

// syncLogFile 确保正常完成和信号取消两条退出路径都把文件日志刷新到磁盘，
// 避免容器关闭时最后一批事件只停留在页缓存中。
func syncLogFile(file *os.File) error {
	if err := file.Sync(); err != nil {
		return fmt.Errorf(
			"run producer: sync NDJSON log file: %w",
			err,
		)
	}

	return nil
}
