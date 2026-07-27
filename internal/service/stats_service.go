package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

// LogStatsReader描述StatsService需要的最小统计读取能力。
type LogStatsReader interface {
	CountByLevel(
		ctx context.Context,
		filter model.LogFilter,
	) ([]model.LogLevelCount, error)

	CountByService(
		ctx context.Context,
		filter model.LogFilter,
	) ([]model.LogServiceCount, error)
}

// StatsInput描述日志统计接口支持的过滤条件。
type StatsInput struct {
	ContainerName string
	Service       string
	Level         string
	Start         *time.Time
	End           *time.Time
}

// LevelStat表示一个日志级别的数量和占比。
type LevelStat struct {
	Level      string
	Count      int64
	Percentage float64
}

// LevelStatsResult表示日志级别统计结果。
type LevelStatsResult struct {
	Stats []LevelStat
	Total int64
}

// ServiceStatsResult表示服务统计结果。
type ServiceStatsResult struct {
	Stats []model.LogServiceCount
	Total int64
}

// StatsService组织日志统计业务。
type StatsService struct {
	logs LogStatsReader
}

// NewStatsService创建日志统计Service。
func NewStatsService(
	logs LogStatsReader,
) (*StatsService, error) {
	if logs == nil {
		return nil, errors.New(
			"create stats service: log repository must not be nil",
		)
	}

	return &StatsService{
		logs: logs,
	}, nil
}

// GetLevelStats查询日志级别统计，并计算每个级别的占比。
func (s *StatsService) GetLevelStats(
	ctx context.Context,
	input StatsInput,
) (LevelStatsResult, error) {
	filter, err := buildStatsFilter(input)
	if err != nil {
		return LevelStatsResult{}, err
	}

	counts, err := s.logs.CountByLevel(ctx, filter)
	if err != nil {
		return LevelStatsResult{}, wrapRepositoryError(
			"get level statistics",
			err,
		)
	}

	var total int64
	for _, item := range counts {
		total += item.Count
	}

	stats := make([]LevelStat, 0, len(counts))
	for _, item := range counts {
		stats = append(stats, LevelStat{
			Level: item.Level,
			Count: item.Count,
			Percentage: calculatePercentage(
				item.Count,
				total,
			),
		})
	}

	return LevelStatsResult{
		Stats: stats,
		Total: total,
	}, nil
}

// GetServiceStats查询按服务分组的日志数量。
func (s *StatsService) GetServiceStats(
	ctx context.Context,
	input StatsInput,
) (ServiceStatsResult, error) {
	filter, err := buildStatsFilter(input)
	if err != nil {
		return ServiceStatsResult{}, err
	}

	counts, err := s.logs.CountByService(ctx, filter)
	if err != nil {
		return ServiceStatsResult{}, wrapRepositoryError(
			"get service statistics",
			err,
		)
	}

	var total int64
	for _, item := range counts {
		total += item.Count
	}

	// 返回新的切片，避免调用方修改Repository返回的切片。
	stats := append([]model.LogServiceCount{}, counts...)

	return ServiceStatsResult{
		Stats: stats,
		Total: total,
	}, nil
}

func buildStatsFilter(
	input StatsInput,
) (model.LogFilter, error) {
	start := cloneTimeAsUTC(input.Start)
	end := cloneTimeAsUTC(input.End)

	if start != nil && end != nil && start.After(*end) {
		return model.LogFilter{}, fmt.Errorf(
			"%w: start must not be later than end",
			ErrInvalidTimeRange,
		)
	}

	return model.LogFilter{
		ContainerName: strings.TrimSpace(
			input.ContainerName,
		),
		Service: strings.TrimSpace(input.Service),
		Level:   normalizeQueryLevel(input.Level),
		Start:   start,
		End:     end,
	}, nil
}

func calculatePercentage(
	count int64,
	total int64,
) float64 {
	if total == 0 {
		return 0
	}

	percentage := float64(count) * 100 / float64(total)

	// 保留一位小数。
	return math.Round(percentage*10) / 10
}
