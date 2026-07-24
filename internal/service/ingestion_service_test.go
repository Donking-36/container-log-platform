package service

import (
	"context"
	"errors"
	"github.com/Donking-36/container-log-platform/internal/ingestion"
	"github.com/Donking-36/container-log-platform/internal/model"
	"testing"
	"time"
)

type fakeLogBatchInserter struct {
	inserted int64
	err      error
	calls    int
	logs     []model.Log
}

type fakeTemporaryRepositoryError struct {
	cause error
}

func (e *fakeTemporaryRepositoryError) Error() string {
	return e.cause.Error()
}

func (e *fakeTemporaryRepositoryError) Unwrap() error {
	return e.cause
}

func (e *fakeTemporaryRepositoryError) Temporary() bool {
	return true
}

func (f *fakeLogBatchInserter) InsertBatch(
	_ context.Context,
	logs []model.Log,
) (int64, error) {
	f.calls++

	// 保存副本，避免测试结果受调用方后续修改影响。
	f.logs = append([]model.Log(nil), logs...)

	return f.inserted, f.err
}

func TestIngestionServiceIngestOne(
	t *testing.T,
) {
	tests := []struct {
		name           string
		repoInserted   int64
		wantInserted   int64
		wantDuplicated int64
	}{
		{
			name:           "new event",
			repoInserted:   1,
			wantInserted:   1,
			wantDuplicated: 0,
		},
		{
			name:           "duplicate event",
			repoInserted:   0,
			wantInserted:   0,
			wantDuplicated: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogBatchInserter{
				inserted: tt.repoInserted,
			}

			service, err := NewIngestionService(
				repo,
				64*1024,
			)
			if err != nil {
				t.Fatalf(
					"NewIngestionService() error: %v",
					err,
				)
			}

			fixedNow := time.Date(
				2026,
				time.July,
				24,
				11,
				0,
				0,
				0,
				time.UTC,
			)

			service.now = func() time.Time {
				return fixedNow
			}

			result, err := service.IngestOne(
				context.Background(),
				validServiceEventInput(),
			)
			if err != nil {
				t.Fatalf(
					"IngestOne() returned an error: %v",
					err,
				)
			}

			if result.Received != 1 {
				t.Fatalf(
					"Received = %d, want 1",
					result.Received,
				)
			}

			if result.Inserted != tt.wantInserted {
				t.Fatalf(
					"Inserted = %d, want %d",
					result.Inserted,
					tt.wantInserted,
				)
			}

			if result.Duplicated != tt.wantDuplicated {
				t.Fatalf(
					"Duplicated = %d, want %d",
					result.Duplicated,
					tt.wantDuplicated,
				)
			}

			if result.Rejected != 0 {
				t.Fatalf(
					"Rejected = %d, want 0",
					result.Rejected,
				)
			}

			if repo.calls != 1 {
				t.Fatalf(
					"repository calls = %d, want 1",
					repo.calls,
				)
			}

			if len(repo.logs) != 1 {
				t.Fatalf(
					"repository logs = %d, want 1",
					len(repo.logs),
				)
			}

			if !repo.logs[0].IngestedAt.Equal(fixedNow) {
				t.Fatalf(
					"IngestedAt = %s, want %s",
					repo.logs[0].IngestedAt,
					fixedNow,
				)
			}
		})
	}
}

func validServiceEventInput() ingestion.EventInput {
	return ingestion.EventInput{
		SourceEventID: "event-001",
		ContainerName: "log-producer-1",
		Service:       "log-producer",
		Level:         "INFO",
		Message:       "service test message",
		Source:        "stdout",
		LoggedAt:      "2026-07-24T10:00:00Z",
	}
}
func TestIngestionServiceIngestOneRejectsInvalidEvent(
	t *testing.T,
) {
	repo := &fakeLogBatchInserter{
		inserted: 1,
	}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	input := validServiceEventInput()
	input.Message = ""

	result, err := service.IngestOne(
		context.Background(),
		input,
	)
	if err == nil {
		t.Fatal("IngestOne() returned nil error")
	}

	var validationErr *ingestion.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf(
			"error type = %T, want wrapped *ValidationError",
			err,
		)
	}

	if validationErr.Field != "message" {
		t.Fatalf(
			"validation field = %q, want message",
			validationErr.Field,
		)
	}

	if result != (IngestResult{}) {
		t.Fatalf(
			"result = %+v, want zero result",
			result,
		)
	}

	if repo.calls != 0 {
		t.Fatalf(
			"repository calls = %d, want 0",
			repo.calls,
		)
	}
}
func TestIngestionServiceIngestOneReturnsRepositoryError(
	t *testing.T,
) {
	repositoryErr := errors.New("database unavailable")

	repo := &fakeLogBatchInserter{
		err: repositoryErr,
	}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	result, err := service.IngestOne(
		context.Background(),
		validServiceEventInput(),
	)
	if err == nil {
		t.Fatal("IngestOne() returned nil error")
	}

	if !errors.Is(err, repositoryErr) {
		t.Fatalf(
			"error = %v, want wrapped repository error",
			err,
		)
	}

	if result != (IngestResult{}) {
		t.Fatalf(
			"result = %+v, want zero result",
			result,
		)
	}

	if repo.calls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			repo.calls,
		)
	}
}
func TestIngestionServiceIngestOneRejectsInvalidInsertedCount(
	t *testing.T,
) {
	tests := []struct {
		name     string
		inserted int64
	}{
		{
			name:     "negative count",
			inserted: -1,
		},
		{
			name:     "count greater than input",
			inserted: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogBatchInserter{
				inserted: tt.inserted,
			}

			service, err := NewIngestionService(
				repo,
				64*1024,
			)
			if err != nil {
				t.Fatalf(
					"NewIngestionService() error: %v",
					err,
				)
			}

			result, err := service.IngestOne(
				context.Background(),
				validServiceEventInput(),
			)
			if err == nil {
				t.Fatal(
					"IngestOne() returned nil error",
				)
			}

			if result != (IngestResult{}) {
				t.Fatalf(
					"result = %+v, want zero result",
					result,
				)
			}
		})
	}
}
func TestNewIngestionServiceValidatesDependencies(
	t *testing.T,
) {
	tests := []struct {
		name               string
		logs               LogBatchInserter
		maxLogMessageBytes int64
	}{
		{
			name:               "nil repository",
			logs:               nil,
			maxLogMessageBytes: 64 * 1024,
		},
		{
			name:               "invalid message limit",
			logs:               &fakeLogBatchInserter{},
			maxLogMessageBytes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewIngestionService(
				tt.logs,
				tt.maxLogMessageBytes,
			)
			if err == nil {
				t.Fatal(
					"NewIngestionService() returned nil error",
				)
			}

			if service != nil {
				t.Fatalf(
					"service = %v, want nil",
					service,
				)
			}
		})
	}
}
func TestIngestionServiceIngestBatchCountsResults(
	t *testing.T,
) {
	repo := &fakeLogBatchInserter{
		// 三条合法事件中，两条新增、一条重复。
		inserted: 2,
	}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	fixedNow := time.Date(
		2026,
		time.July,
		24,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	clockCalls := 0
	service.now = func() time.Time {
		clockCalls++
		return fixedNow
	}

	first := serviceEventInputWithID("event-001")
	second := serviceEventInputWithID("event-002")
	third := serviceEventInputWithID("event-003")

	invalid := serviceEventInputWithID("event-invalid")
	invalid.Message = ""

	result, err := service.IngestBatch(
		context.Background(),
		[]ingestion.EventInput{
			first,
			invalid,
			second,
			third,
		},
	)
	if err != nil {
		t.Fatalf(
			"IngestBatch() returned an error: %v",
			err,
		)
	}

	if result.Received != 4 {
		t.Fatalf(
			"Received = %d, want 4",
			result.Received,
		)
	}

	if result.Inserted != 2 {
		t.Fatalf(
			"Inserted = %d, want 2",
			result.Inserted,
		)
	}

	if result.Duplicated != 1 {
		t.Fatalf(
			"Duplicated = %d, want 1",
			result.Duplicated,
		)
	}

	if result.Rejected != 1 {
		t.Fatalf(
			"Rejected = %d, want 1",
			result.Rejected,
		)
	}

	if result.Received !=
		result.Inserted+
			result.Duplicated+
			result.Rejected {
		t.Fatalf(
			"result invariant failed: %+v",
			result,
		)
	}

	if repo.calls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			repo.calls,
		)
	}

	if len(repo.logs) != 3 {
		t.Fatalf(
			"repository logs = %d, want 3 valid logs",
			len(repo.logs),
		)
	}

	if clockCalls != 1 {
		t.Fatalf(
			"clock calls = %d, want 1",
			clockCalls,
		)
	}

	for index, entry := range repo.logs {
		if !entry.IngestedAt.Equal(fixedNow) {
			t.Fatalf(
				"log %d IngestedAt = %s, want %s",
				index,
				entry.IngestedAt,
				fixedNow,
			)
		}
	}
}
func TestIngestionServiceIngestBatchAllowsAllRejected(
	t *testing.T,
) {
	repo := &fakeLogBatchInserter{
		inserted: 100,
	}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	first := serviceEventInputWithID("event-001")
	first.Message = ""

	second := serviceEventInputWithID("event-002")
	second.LoggedAt = "invalid-time"

	result, err := service.IngestBatch(
		context.Background(),
		[]ingestion.EventInput{
			first,
			second,
		},
	)
	if err != nil {
		t.Fatalf(
			"IngestBatch() returned an error: %v",
			err,
		)
	}

	if result != (IngestResult{
		Received: 2,
		Rejected: 2,
	}) {
		t.Fatalf(
			"result = %+v, want received=2 rejected=2",
			result,
		)
	}

	if repo.calls != 0 {
		t.Fatalf(
			"repository calls = %d, want 0",
			repo.calls,
		)
	}
}
func TestIngestionServiceIngestBatchRejectsEmptyBatch(
	t *testing.T,
) {
	repo := &fakeLogBatchInserter{}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	result, err := service.IngestBatch(
		context.Background(),
		nil,
	)
	if !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf(
			"error = %v, want ErrEmptyBatch",
			err,
		)
	}

	if result != (IngestResult{}) {
		t.Fatalf(
			"result = %+v, want zero result",
			result,
		)
	}

	if repo.calls != 0 {
		t.Fatalf(
			"repository calls = %d, want 0",
			repo.calls,
		)
	}
}
func TestIngestionServiceIngestBatchReturnsRepositoryError(
	t *testing.T,
) {
	repositoryErr := errors.New("database unavailable")

	repo := &fakeLogBatchInserter{
		err: repositoryErr,
	}

	service, err := NewIngestionService(
		repo,
		64*1024,
	)
	if err != nil {
		t.Fatalf(
			"NewIngestionService() error: %v",
			err,
		)
	}

	result, err := service.IngestBatch(
		context.Background(),
		[]ingestion.EventInput{
			serviceEventInputWithID("event-001"),
			serviceEventInputWithID("event-002"),
		},
	)
	if !errors.Is(err, repositoryErr) {
		t.Fatalf(
			"error = %v, want wrapped repository error",
			err,
		)
	}

	if result != (IngestResult{}) {
		t.Fatalf(
			"result = %+v, want zero result",
			result,
		)
	}

	if repo.calls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			repo.calls,
		)
	}
}

func TestIngestionServiceClassifiesTemporaryRepositoryError(
	t *testing.T,
) {
	tests := []struct {
		name   string
		ingest func(*IngestionService) (IngestResult, error)
	}{
		{
			name: "single event",
			ingest: func(
				service *IngestionService,
			) (IngestResult, error) {
				return service.IngestOne(
					context.Background(),
					validServiceEventInput(),
				)
			},
		},
		{
			name: "batch",
			ingest: func(
				service *IngestionService,
			) (IngestResult, error) {
				return service.IngestBatch(
					context.Background(),
					[]ingestion.EventInput{
						serviceEventInputWithID("event-001"),
					},
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := errors.New("database is locked")
			repositoryErr := &fakeTemporaryRepositoryError{
				cause: cause,
			}
			repo := &fakeLogBatchInserter{
				err: repositoryErr,
			}

			ingestionService, err := NewIngestionService(
				repo,
				64*1024,
			)
			if err != nil {
				t.Fatalf(
					"NewIngestionService() error: %v",
					err,
				)
			}

			result, err := tt.ingest(ingestionService)

			if !errors.Is(
				err,
				ErrTemporarilyUnavailable,
			) {
				t.Fatalf(
					"error = %v, want temporary unavailable",
					err,
				)
			}

			if !errors.Is(err, cause) {
				t.Fatalf(
					"error = %v, want wrapped cause",
					err,
				)
			}

			if result != (IngestResult{}) {
				t.Fatalf(
					"result = %+v, want zero result",
					result,
				)
			}
		})
	}
}

func serviceEventInputWithID(
	sourceEventID string,
) ingestion.EventInput {
	input := validServiceEventInput()
	input.SourceEventID = sourceEventID

	return input
}
