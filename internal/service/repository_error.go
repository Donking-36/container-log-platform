package service

import (
	"errors"
	"fmt"
)

// ErrTemporarilyUnavailable 表示数据库临时繁忙等可重试故障。
// Handler 会将它映射为 HTTP 503，使 Logstash 只重试临时失败，
// 而不会把永久数据错误放入无限重试循环。
var ErrTemporarilyUnavailable = errors.New(
	"log service temporarily unavailable",
)

type temporaryError interface {
	error
	Temporary() bool
}

func wrapRepositoryError(
	operation string,
	err error,
) error {
	var temporary temporaryError
	// Service 只依赖 Temporary 能力，不需要知道底层是 SQLite，
	// 从而保持错误分类与存储实现解耦。
	if errors.As(err, &temporary) && temporary.Temporary() {
		err = errors.Join(
			ErrTemporarilyUnavailable,
			err,
		)
	}

	return fmt.Errorf("%s: %w", operation, err)
}
