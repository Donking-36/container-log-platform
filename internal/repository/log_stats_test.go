package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

func TestLogRepositoryCountByLevel(t *testing.T) {
	repository := newStatsTestRepository(t)

	baseTime := time.Date(
		2026,
		time.July,
		27,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	seedStatsLogs(
		t,
		repository,
		[]model.Log{
			newStatsLog(
				"event-1",
				"container-a",
				"api",
				"INFO",
				baseTime,
			),
			newStatsLog(
				"event-2",
				"container-a",
				"api",
				"INFO",
				baseTime.Add(time.Minute),
			),
			newStatsLog(
				"event-3",
				"container-a",
				"worker",
				"ERROR",
				baseTime.Add(2*time.Minute),
			),
			newStatsLog(
				"event-4",
				"container-a",
				"worker",
				"WARN",
				baseTime.Add(3*time.Minute),
			),

			// 容器不匹配，应被过滤。
			newStatsLog(
				"event-5",
				"container-b",
				"api",
				"ERROR",
				baseTime.Add(4*time.Minute),
			),

			// 时间超出范围，应被过滤。
			newStatsLog(
				"event-6",
				"container-a",
				"worker",
				"INFO",
				baseTime.Add(2*time.Hour),
			),
		},
	)

	start := baseTime
	end := baseTime.Add(10 * time.Minute)

	got, err := repository.CountByLevel(
		context.Background(),
		model.LogFilter{
			ContainerName: "container-a",
			Start:         &start,
			End:           &end,
		},
	)
	if err != nil {
		t.Fatalf("count logs by level: %v", err)
	}

	want := []model.LogLevelCount{
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
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"level stats = %#v, want %#v",
			got,
			want,
		)
	}
}

func TestLogRepositoryCountByService(t *testing.T) {
	repository := newStatsTestRepository(t)

	baseTime := time.Date(
		2026,
		time.July,
		27,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	seedStatsLogs(
		t,
		repository,
		[]model.Log{
			newStatsLog(
				"event-1",
				"container-a",
				"api",
				"INFO",
				baseTime,
			),
			newStatsLog(
				"event-2",
				"container-a",
				"api",
				"ERROR",
				baseTime.Add(time.Minute),
			),
			newStatsLog(
				"event-3",
				"container-a",
				"worker",
				"INFO",
				baseTime.Add(2*time.Minute),
			),
			newStatsLog(
				"event-4",
				"container-a",
				"worker",
				"WARN",
				baseTime.Add(3*time.Minute),
			),
			newStatsLog(
				"event-5",
				"container-b",
				"other",
				"INFO",
				baseTime.Add(4*time.Minute),
			),
		},
	)

	got, err := repository.CountByService(
		context.Background(),
		model.LogFilter{
			ContainerName: "container-a",
		},
	)
	if err != nil {
		t.Fatalf("count logs by service: %v", err)
	}

	// api和worker数量相同，因此按服务名升序排列。
	want := []model.LogServiceCount{
		{
			Service: "api",
			Count:   2,
		},
		{
			Service: "worker",
			Count:   2,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf(
			"service stats = %#v, want %#v",
			got,
			want,
		)
	}
}

func TestLogRepositoryStatisticsReturnEmptySlice(
	t *testing.T,
) {
	repository := newStatsTestRepository(t)

	levelStats, err := repository.CountByLevel(
		context.Background(),
		model.LogFilter{
			Service: "missing-service",
		},
	)
	if err != nil {
		t.Fatalf("count logs by level: %v", err)
	}

	if levelStats == nil || len(levelStats) != 0 {
		t.Fatalf(
			"level stats = %#v, want non-nil empty slice",
			levelStats,
		)
	}

	serviceStats, err := repository.CountByService(
		context.Background(),
		model.LogFilter{
			Service: "missing-service",
		},
	)
	if err != nil {
		t.Fatalf("count logs by service: %v", err)
	}

	if serviceStats == nil || len(serviceStats) != 0 {
		t.Fatalf(
			"service stats = %#v, want non-nil empty slice",
			serviceStats,
		)
	}
}

func newStatsTestRepository(
	t *testing.T,
) *LogRepository {
	t.Helper()

	gormDB, err := OpenSQLite(
		filepath.Join(t.TempDir(), "stats.db"),
	)
	if err != nil {
		t.Fatalf("open test SQLite database: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("access test SQLite connection pool: %v", err)
	}

	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close test SQLite database: %v", err)
		}
	})

	if err := Migrate(gormDB); err != nil {
		t.Fatalf("migrate test SQLite database: %v", err)
	}

	repository, err := NewLogRepository(gormDB)
	if err != nil {
		t.Fatalf("create test log repository: %v", err)
	}

	return repository
}

func seedStatsLogs(
	t *testing.T,
	repository *LogRepository,
	logs []model.Log,
) {
	t.Helper()

	if err := repository.db.Create(&logs).Error; err != nil {
		t.Fatalf("seed statistics logs: %v", err)
	}
}

func newStatsLog(
	eventID string,
	containerName string,
	serviceName string,
	level string,
	loggedAt time.Time,
) model.Log {
	return model.Log{
		EventID:       eventID,
		ContainerName: containerName,
		Service:       serviceName,
		Level:         level,
		Message:       "statistics test log",
		Source:        "stdout",
		LoggedAt:      loggedAt,
		IngestedAt:    loggedAt.Add(time.Second),
	}
}
