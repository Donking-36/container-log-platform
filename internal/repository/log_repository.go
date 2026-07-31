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

// temporaryStorageError 标记 SQLite 的 BUSY/LOCKED 错误，使上层可以在
// 不依赖具体数据库驱动的前提下将故障分类为“可重试”。
type temporaryStorageError struct {
	err error
}

// Error 返回底层存储错误文本。
func (e *temporaryStorageError) Error() string {
	return e.err.Error()
}

// Unwrap 保留底层错误链，供 errors.Is 和 errors.As 继续匹配。
func (e *temporaryStorageError) Unwrap() error {
	return e.err
}

// Temporary 报告该存储故障是否适合稍后重试。
func (e *temporaryStorageError) Temporary() bool {
	return true
}

type sqliteCodeError interface {
	error
	Code() int
}

// LogRepository 负责日志记录的 SQLite 持久化。
type LogRepository struct {
	db *gorm.DB
}

// NewLogRepository 创建日志 Repository。
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
// 整批写入由一条 GORM Create 操作完成；event_id 冲突只跳过重复项，
// 其他数据库错误仍使整个调用失败。
func (r *LogRepository) InsertBatch(
	ctx context.Context,
	logs []model.Log,
) (int64, error) {
	if len(logs) == 0 {
		return 0, nil
	}

	records := slices.Clone(logs)

	// ID 由 SQLite 生成，不能接受调用方指定的数据库 ID。
	for index := range records {
		records[index].ID = 0
	}

	// 唯一索引与 ON CONFLICT DO NOTHING 共同完成原子去重，
	// 避免“先查询再插入”存在的并发竞态。
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

	// SQLITE_BUSY/LOCKED 通常由短暂写锁竞争引起，调用方可以重试；
	// 语法、约束和磁盘损坏等错误则不能按临时故障处理。
	// 扩展错误码的低 8 位仍是基础 SQLite 结果码。
	baseCode := sqliteErr.Code() & 0xff

	return baseCode == sqlite3.SQLITE_BUSY ||
		baseCode == sqlite3.SQLITE_LOCKED
}
