package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

type fakeLogStatsReader struct {
	levelCounts   []model.LogLevelCount
	serviceCounts []model.LogServiceCount

	levelErr   error
	serviceErr error

	levelCalls   int
	serviceCalls int

	levelContext   context.Context
	serviceContext context.Context

	levelFilter   model.LogFilter
	serviceFilter model.LogFilter
}

func (f *fakeLogStatsReader) CountByLevel(
	ctx context.Context,
	filter model.LogFilter,
) ([]model.LogLevelCount, error) {
	f.levelCalls++
	f.levelContext = ctx
	f.levelFilter = filter

	return f.levelCounts, f.levelErr
}

func (f *fakeLogStatsReader) CountByService(
	ctx context.Context,
	filter model.LogFilter,
) ([]model.LogServiceCount, error) {
	f.serviceCalls++
	f.serviceContext = ctx
	f.serviceFilter = filter

	return f.serviceCounts, f.serviceErr
}

type statsContextKey struct{}

type statsTemporaryError struct {
	err error
}

func (e *statsTemporaryError) Error() string {
	return e.err.Error()
}

func (e *statsTemporaryError) Unwrap() error {
	return e.err
}

func (e *statsTemporaryError) Temporary() bool {
	return true
}

func TestNewStatsServiceRejectsNilRepository(t *testing.T) {
	statsService, err := NewStatsService(nil)
	if err == nil {
		t.Fatal("NewStatsService(nil) returned nil error")
	}

	if statsService != nil {
		t.Fatalf(
			"service = %#v, want nil",
			statsService,
		)
	}
}

func TestStatsServiceGetLevelStats(t *testing.T) {
	reader := &fakeLogStatsReader{
		levelCounts: []model.LogLevelCount{
			{
				Level: "INFO",
				Count: 2,
			},
			{
				Level: "ERROR",
				Count: 1,
			},
			{
				Level: "WARN",
				Count: 1,
			},
		},
	}

	statsService, err := NewStatsService(reader)
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}

	location := time.FixedZone("UTC+8", 8*60*60)
	start := time.Date(
		2026,
		time.July,
		27,
		10,
		0,
		0,
		0,
		location,
	)
	end := start.Add(time.Hour)

	ctx := context.WithValue(
		context.Background(),
		statsContextKey{},
		"stats-context",
	)

	result, err := statsService.GetLevelStats(
		ctx,
		StatsInput{
			ContainerName: " container-a ",
			Service:       " api ",
			Level:         " error ",
			Start:         &start,
			End:           &end,
		},
	)
	if err != nil {
		t.Fatalf("get level statistics: %v", err)
	}

	want := LevelStatsResult{
		Stats: []LevelStat{
			{
				Level:      "INFO",
				Count:      2,
				Percentage: 50,
			},
			{
				Level:      "ERROR",
				Count:      1,
				Percentage: 25,
			},
			{
				Level:      "WARN",
				Count:      1,
				Percentage: 25,
			},
		},
		Total: 4,
	}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf(
			"level statistics = %#v, want %#v",
			result,
			want,
		)
	}

	if reader.levelCalls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			reader.levelCalls,
		)
	}

	if reader.levelContext.Value(
		statsContextKey{},
	) != "stats-context" {
		t.Fatal("service did not preserve request context")
	}

	filter := reader.levelFilter

	if filter.ContainerName != "container-a" {
		t.Fatalf(
			"container filter = %q, want container-a",
			filter.ContainerName,
		)
	}

	if filter.Service != "api" {
		t.Fatalf(
			"service filter = %q, want api",
			filter.Service,
		)
	}

	if filter.Level != "ERROR" {
		t.Fatalf(
			"level filter = %q, want ERROR",
			filter.Level,
		)
	}

	if filter.Start == nil ||
		!filter.Start.Equal(start) ||
		filter.Start.Location() != time.UTC {
		t.Fatalf(
			"start filter = %v, want UTC %v",
			filter.Start,
			start.UTC(),
		)
	}

	if filter.End == nil ||
		!filter.End.Equal(end) ||
		filter.End.Location() != time.UTC {
		t.Fatalf(
			"end filter = %v, want UTC %v",
			filter.End,
			end.UTC(),
		)
	}
}

func TestStatsServiceGetServiceStats(t *testing.T) {
	reader := &fakeLogStatsReader{
		serviceCounts: []model.LogServiceCount{
			{
				Service: "api",
				Count:   2,
			},
			{
				Service: "worker",
				Count:   1,
			},
		},
	}

	statsService, err := NewStatsService(reader)
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}

	result, err := statsService.GetServiceStats(
		context.Background(),
		StatsInput{
			ContainerName: "container-a",
		},
	)
	if err != nil {
		t.Fatalf("get service statistics: %v", err)
	}

	want := ServiceStatsResult{
		Stats: []model.LogServiceCount{
			{
				Service: "api",
				Count:   2,
			},
			{
				Service: "worker",
				Count:   1,
			},
		},
		Total: 3,
	}

	if !reflect.DeepEqual(result, want) {
		t.Fatalf(
			"service statistics = %#v, want %#v",
			result,
			want,
		)
	}

	if reader.serviceCalls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			reader.serviceCalls,
		)
	}

	// Service应返回新切片，避免调用方修改Repository数据。
	result.Stats[0].Count = 99

	if reader.serviceCounts[0].Count != 2 {
		t.Fatal(
			"service result shares Repository slice",
		)
	}
}

func TestStatsServiceReturnsEmptyResults(t *testing.T) {
	reader := &fakeLogStatsReader{}

	statsService, err := NewStatsService(reader)
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}

	levelResult, err := statsService.GetLevelStats(
		context.Background(),
		StatsInput{},
	)
	if err != nil {
		t.Fatalf("get level statistics: %v", err)
	}

	if levelResult.Total != 0 ||
		levelResult.Stats == nil ||
		len(levelResult.Stats) != 0 {
		t.Fatalf(
			"level result = %#v, want non-nil empty result",
			levelResult,
		)
	}

	serviceResult, err := statsService.GetServiceStats(
		context.Background(),
		StatsInput{},
	)
	if err != nil {
		t.Fatalf("get service statistics: %v", err)
	}

	if serviceResult.Total != 0 ||
		serviceResult.Stats == nil ||
		len(serviceResult.Stats) != 0 {
		t.Fatalf(
			"service result = %#v, want non-nil empty result",
			serviceResult,
		)
	}
}

func TestStatsServiceRejectsInvalidTimeRange(
	t *testing.T,
) {
	reader := &fakeLogStatsReader{}

	statsService, err := NewStatsService(reader)
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}

	start := time.Date(
		2026,
		time.July,
		27,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	end := start.Add(-time.Minute)

	_, err = statsService.GetLevelStats(
		context.Background(),
		StatsInput{
			Start: &start,
			End:   &end,
		},
	)
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf(
			"error = %v, want ErrInvalidTimeRange",
			err,
		)
	}

	if reader.levelCalls != 0 {
		t.Fatalf(
			"repository calls = %d, want 0",
			reader.levelCalls,
		)
	}
}

func TestStatsServiceMapsTemporaryRepositoryError(
	t *testing.T,
) {
	databaseErr := errors.New("database is busy")

	reader := &fakeLogStatsReader{
		levelErr: &statsTemporaryError{
			err: databaseErr,
		},
	}

	statsService, err := NewStatsService(reader)
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}

	_, err = statsService.GetLevelStats(
		context.Background(),
		StatsInput{},
	)
	if !errors.Is(err, ErrTemporarilyUnavailable) {
		t.Fatalf(
			"error = %v, want ErrTemporarilyUnavailable",
			err,
		)
	}

	if !errors.Is(err, databaseErr) {
		t.Fatalf(
			"error = %v, want original database error",
			err,
		)
	}
}

func TestCalculatePercentage(t *testing.T) {
	tests := []struct {
		name  string
		count int64
		total int64
		want  float64
	}{
		{
			name:  "half",
			count: 1,
			total: 2,
			want:  50,
		},
		{
			name:  "rounded to one decimal",
			count: 1,
			total: 3,
			want:  33.3,
		},
		{
			name:  "zero total",
			count: 0,
			total: 0,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePercentage(
				tt.count,
				tt.total,
			)

			if got != tt.want {
				t.Fatalf(
					"percentage = %v, want %v",
					got,
					tt.want,
				)
			}
		})
	}
}
