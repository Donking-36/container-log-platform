package repository

import (
	"context"

	"github.com/Donking-36/container-log-platform/internal/model"
)

// CountByLevel 按日志级别聚合日志数量，并以数量降序、级别升序稳定排序。
func (r *LogRepository) CountByLevel(
	ctx context.Context,
	filter model.LogFilter,
) ([]model.LogLevelCount, error) {
	stats := make([]model.LogLevelCount, 0)

	err := applyLogFilter(
		r.db.WithContext(ctx).Model(&model.Log{}),
		filter,
	).
		Select("level, COUNT(*) AS count").
		Group("level").
		Order("count DESC").
		Order("level ASC").
		Scan(&stats).
		Error
	if err != nil {
		return nil, wrapLogReadError(
			"count logs by level",
			err,
		)
	}

	return stats, nil
}

// CountByService 按服务名称聚合日志数量，并以数量降序、服务名升序稳定排序。
func (r *LogRepository) CountByService(
	ctx context.Context,
	filter model.LogFilter,
) ([]model.LogServiceCount, error) {
	stats := make([]model.LogServiceCount, 0)

	err := applyLogFilter(
		r.db.WithContext(ctx).Model(&model.Log{}),
		filter,
	).
		Select("service, COUNT(*) AS count").
		Group("service").
		Order("count DESC").
		Order("service ASC").
		Scan(&stats).
		Error
	if err != nil {
		return nil, wrapLogReadError(
			"count logs by service",
			err,
		)
	}

	return stats, nil
}
