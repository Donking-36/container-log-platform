package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
)

type missingLogError struct {
	err error
}

func (e *missingLogError) Error() string {
	return e.err.Error()
}

func (e *missingLogError) Unwrap() error {
	return e.err
}

func (e *missingLogError) NotFound() bool {
	return true
}

// List按固定条件查询日志，并返回分页前的匹配总数。
func (r *LogRepository) List(
	ctx context.Context,
	query model.LogListQuery,
) ([]model.Log, int64, error) {
	if query.Limit <= 0 {
		return nil, 0, fmt.Errorf(
			"list logs: limit must be greater than zero",
		)
	}

	if query.Offset < 0 {
		return nil, 0, fmt.Errorf(
			"list logs: offset must not be negative",
		)
	}

	logs := make([]model.Log, 0)
	var total int64

	// Count和当前页共用同一个事务，保证一次响应看到同一读取快照。
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		countQuery := applyLogFilter(
			tx.Model(&model.Log{}),
			query.Filter,
		)

		if err := countQuery.Count(&total).Error; err != nil {
			return fmt.Errorf(
				"count matching logs: %w",
				err,
			)
		}

		if total == 0 {
			return nil
		}

		dataQuery := applyLogFilter(
			tx.Model(&model.Log{}),
			query.Filter,
		)

		if err := dataQuery.
			Omit("raw_event").
			Order("logged_at DESC").
			Order("id DESC").
			Limit(query.Limit).
			Offset(query.Offset).
			Find(&logs).
			Error; err != nil {
			return fmt.Errorf(
				"read matching logs: %w",
				err,
			)
		}

		return nil
	})
	if err != nil {
		return nil, 0, wrapLogReadError(
			"list logs",
			err,
		)
	}

	return logs, total, nil
}

// FindByID按SQLite主键读取一条完整日志。
func (r *LogRepository) FindByID(
	ctx context.Context,
	id int64,
) (model.Log, error) {
	if id <= 0 {
		return model.Log{}, fmt.Errorf(
			"find log by ID: id must be greater than zero",
		)
	}

	var logEntry model.Log
	if err := r.db.WithContext(ctx).
		Take(&logEntry, "id = ?", id).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = &missingLogError{
				err: err,
			}
		}

		return model.Log{}, wrapLogReadError(
			"find log by ID",
			err,
		)
	}

	return logEntry, nil
}

func applyLogFilter(
	db *gorm.DB,
	filter model.LogFilter,
) *gorm.DB {
	if filter.ContainerName != "" {
		db = db.Where(
			"container_name = ?",
			filter.ContainerName,
		)
	}

	if filter.Service != "" {
		db = db.Where("service = ?", filter.Service)
	}

	if filter.Level != "" {
		db = db.Where("level = ?", filter.Level)
	}

	if filter.Start != nil {
		db = db.Where(
			"logged_at >= ?",
			filter.Start.UTC(),
		)
	}

	if filter.End != nil {
		db = db.Where(
			"logged_at <= ?",
			filter.End.UTC(),
		)
	}

	return db
}

func wrapLogReadError(
	operation string,
	err error,
) error {
	wrapped := fmt.Errorf("%s: %w", operation, err)

	if !isTemporarySQLiteError(err) {
		return wrapped
	}

	return &temporaryStorageError{
		err: wrapped,
	}
}
