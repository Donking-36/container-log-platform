package service

import (
	"errors"
	"fmt"
)

// ErrTemporarilyUnavailable表示数据库临时繁忙等可重试故障。
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
	if errors.As(err, &temporary) && temporary.Temporary() {
		err = errors.Join(
			ErrTemporarilyUnavailable,
			err,
		)
	}

	return fmt.Errorf("%s: %w", operation, err)
}
