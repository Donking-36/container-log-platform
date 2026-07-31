package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
)

// missingLogError 将驱动层的“记录不存在”转换成不暴露 GORM 的能力接口，
// 由 Service 再映射为稳定的领域错误。
type missingLogError struct {
	err error
}

// Error 返回底层“记录不存在”错误文本。
func (e *missingLogError) Error() string {
	return e.err.Error()
}

// Unwrap 保留底层错误链，供 errors.Is 和 errors.As 继续匹配。
func (e *missingLogError) Unwrap() error {
	return e.err
}

// NotFound 报告该错误代表目标日志不存在。
func (e *missingLogError) NotFound() bool {
	return true
}

// List 按固定条件查询日志，并返回分页前的匹配总数。
// 列表结果固定按 logged_at、id 倒序排列，以保证相同时间戳下仍可稳定分页。
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

	// Count 和当前页共用同一个事务，保证一次响应看到同一读取快照。
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

		// 列表页不返回体积较大的原始事件；详情查询仍会读取完整记录。
		// id 作为第二排序键，解决多条日志 LoggedAt 相同时顺序不确定的问题。
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

// FindByID 按 SQLite 主键读取一条完整日志，包括 RawEvent。
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
	// 所有值都通过占位符绑定；这里只允许固定字段，不能把客户端输入
	// 直接拼接进 SQL。
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
