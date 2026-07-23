package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
		return 0, fmt.Errorf(
			"insert log batch: %w",
			result.Error,
		)
	}

	return result.RowsAffected, nil
}
