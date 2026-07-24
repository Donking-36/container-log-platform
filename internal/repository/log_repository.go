package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	sqlite3 "modernc.org/sqlite/lib"
)

type temporaryStorageError struct {
	err error
}

func (e *temporaryStorageError) Error() string {
	return e.err.Error()
}

func (e *temporaryStorageError) Unwrap() error {
	return e.err
}

func (e *temporaryStorageError) Temporary() bool {
	return true
}

type sqliteCodeError interface {
	error
	Code() int
}

// LogRepository 负责日志记录的SQLite持久化。
type LogRepository struct {
	db *gorm.DB
}

// NewLogRepository 创建日志Repository。
func NewLogRepository(db *gorm.DB) (*LogRepository, error) {
	if db == nil {
		return nil, errors.New(
			"create log repository: database must not be nil",
		)
	}

	return &LogRepository{
		db: db,
	}, nil
}

// InsertBatch 幂等插入一批日志，并返回本次实际新增的数量。
func (r *LogRepository) InsertBatch(
	ctx context.Context,
	logs []model.Log,
) (int64, error) {
	if len(logs) == 0 {
		return 0, nil
	}

	records := slices.Clone(logs)

	// ID由SQLite生成，不能接受调用方指定的数据库ID。
	for index := range records {
		records[index].ID = 0
	}

	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "event_id"},
			},
			DoNothing: true,
		}).
		Create(&records)

	if result.Error != nil {
		return 0, wrapLogInsertError(result.Error)
	}

	return result.RowsAffected, nil
}

func wrapLogInsertError(err error) error {
	wrapped := fmt.Errorf(
		"insert log batch: %w",
		err,
	)

	if !isTemporarySQLiteError(err) {
		return wrapped
	}

	return &temporaryStorageError{
		err: wrapped,
	}
}

func isTemporarySQLiteError(err error) bool {
	var sqliteErr sqliteCodeError
	if !errors.As(err, &sqliteErr) {
		return false
	}

	// 扩展错误码的低8位仍是基础SQLite结果码。
	baseCode := sqliteErr.Code() & 0xff

	return baseCode == sqlite3.SQLITE_BUSY ||
		baseCode == sqlite3.SQLITE_LOCKED
}
