package service

import (
	"context"
	"fmt"
	"github.com/Donking-36/container-log-platform/internal/ingestion"
	"github.com/Donking-36/container-log-platform/internal/model"
	"github.com/Donking-36/container-log-platform/internal/repository"
	"gorm.io/gorm"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIngestionServiceIngestBatchIsIdempotentWithSQLite(
	t *testing.T,
) {
	service, db := newSQLiteIngestionServiceForTest(t)

	inputs := make(
		[]ingestion.EventInput,
		0,
		100,
	)

	for number := 1; number <= 100; number++ {
		input := validServiceEventInput()
		input.SourceEventID = fmt.Sprintf(
			"event-%06d",
			number,
		)
		input.Message = fmt.Sprintf(
			"test message %d",
			number,
		)

		inputs = append(inputs, input)
	}

	firstIngestedAt := time.Date(
		2026,
		time.July,
		24,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	service.now = func() time.Time {
		return firstIngestedAt
	}

	first, err := service.IngestBatch(
		context.Background(),
		inputs,
	)
	if err != nil {
		t.Fatalf(
			"first IngestBatch() returned an error: %v",
			err,
		)
	}

	wantFirst := IngestResult{
		Received:   100,
		Inserted:   100,
		Duplicated: 0,
		Rejected:   0,
	}

	if first != wantFirst {
		t.Fatalf(
			"first result = %+v, want %+v",
			first,
			wantFirst,
		)
	}

	// 第二次投递发生在不同时间，
	// 但ingested_at不参与event_id，因此仍应识别为重复。
	secondIngestedAt := firstIngestedAt.Add(
		time.Hour,
	)

	service.now = func() time.Time {
		return secondIngestedAt
	}

	second, err := service.IngestBatch(
		context.Background(),
		inputs,
	)
	if err != nil {
		t.Fatalf(
			"second IngestBatch() returned an error: %v",
			err,
		)
	}

	wantSecond := IngestResult{
		Received:   100,
		Inserted:   0,
		Duplicated: 100,
		Rejected:   0,
	}

	if second != wantSecond {
		t.Fatalf(
			"second result = %+v, want %+v",
			second,
			wantSecond,
		)
	}

	var storedCount int64
	if err := db.
		Model(&model.Log{}).
		Count(&storedCount).
		Error; err != nil {
		t.Fatalf(
			"count stored logs: %v",
			err,
		)
	}

	if storedCount != 100 {
		t.Fatalf(
			"stored log count = %d, want 100",
			storedCount,
		)
	}

	var distinctEventIDs int64
	if err := db.
		Model(&model.Log{}).
		Distinct("event_id").
		Count(&distinctEventIDs).
		Error; err != nil {
		t.Fatalf(
			"count distinct event IDs: %v",
			err,
		)
	}

	if distinctEventIDs != 100 {
		t.Fatalf(
			"distinct event IDs = %d, want 100",
			distinctEventIDs,
		)
	}
}

func newSQLiteIngestionServiceForTest(
	t *testing.T,
) (*IngestionService, *gorm.DB) {
	t.Helper()

	databasePath := filepath.Join(
		t.TempDir(),
		"log-platform.db",
	)

	db, err := repository.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf(
			"repository.OpenSQLite() error: %v",
			err,
		)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf(
			"access database connection pool: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf(
				"close database connection: %v",
				err,
			)
		}
	})

	if err := repository.Migrate(db); err != nil {
		t.Fatalf(
			"repository.Migrate() error: %v",
			err,
		)
	}

	logRepository, err := repository.NewLogRepository(db)
	if err != nil {
		t.Fatalf(
			"repository.NewLogRepository() error: %v",
			err,
		)
	}

	service, err := NewIngestionService(
		logRepository,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	return service, db
}
func TestIngestionServiceIngestOneIsIdempotentUnderConcurrency(
	t *testing.T,
) {
	service, db := newSQLiteIngestionServiceForTest(t)

	fixedNow := time.Date(
		2026,
		time.July,
		24,
		14,
		0,
		0,
		0,
		time.UTC,
	)

	// 所有goroutine只读取这个函数，不在并发期间修改它。
	service.now = func() time.Time {
		return fixedNow
	}

	const workers = 20

	type outcome struct {
		result IngestResult
		err    error
	}

	// start用于让所有goroutine尽量同时开始。
	start := make(chan struct{})

	// 使用带缓冲的channel，避免goroutine发送结果时阻塞。
	outcomes := make(
		chan outcome,
		workers,
	)

	var waitGroup sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			// start关闭之前，所有goroutine都等待在这里。
			<-start

			result, err := service.IngestOne(
				context.Background(),
				serviceEventInputWithID(
					"concurrent-event-001",
				),
			)

			outcomes <- outcome{
				result: result,
				err:    err,
			}
		}()
	}

	// 关闭channel会同时唤醒所有等待接收的goroutine。
	close(start)

	waitGroup.Wait()
	close(outcomes)

	var totalReceived int64
	var totalInserted int64
	var totalDuplicated int64
	var totalRejected int64

	// 这里已经回到测试主goroutine，可以安全调用t.Fatalf。
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf(
				"IngestOne() returned an error: %v",
				outcome.err,
			)
		}

		if outcome.result.Received != 1 {
			t.Fatalf(
				"individual Received = %d, want 1",
				outcome.result.Received,
			)
		}

		if outcome.result.Inserted+
			outcome.result.Duplicated != 1 {
			t.Fatalf(
				"invalid individual result: %+v",
				outcome.result,
			)
		}

		totalReceived += outcome.result.Received
		totalInserted += outcome.result.Inserted
		totalDuplicated += outcome.result.Duplicated
		totalRejected += outcome.result.Rejected
	}

	if totalReceived != workers {
		t.Fatalf(
			"total Received = %d, want %d",
			totalReceived,
			workers,
		)
	}

	if totalInserted != 1 {
		t.Fatalf(
			"total Inserted = %d, want 1",
			totalInserted,
		)
	}

	if totalDuplicated != workers-1 {
		t.Fatalf(
			"total Duplicated = %d, want %d",
			totalDuplicated,
			workers-1,
		)
	}

	if totalRejected != 0 {
		t.Fatalf(
			"total Rejected = %d, want 0",
			totalRejected,
		)
	}

	var storedCount int64
	if err := db.
		Model(&model.Log{}).
		Count(&storedCount).
		Error; err != nil {
		t.Fatalf(
			"count stored logs: %v",
			err,
		)
	}

	if storedCount != 1 {
		t.Fatalf(
			"stored log count = %d, want 1",
			storedCount,
		)
	}
}
