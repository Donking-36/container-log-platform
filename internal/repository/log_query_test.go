package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestLogRepositoryListFiltersSortsAndPaginates(
	t *testing.T,
) {
	repo, db := newLogQueryRepositoryForTest(t)
	seedLogQueryRecords(t, repo)

	start := logQueryTestTime(1)
	end := logQueryTestTime(2)

	tests := []struct {
		name         string
		query        model.LogListQuery
		wantEventIDs []string
		wantTotal    int64
	}{
		{
			name: "all logs use stable descending order",
			query: model.LogListQuery{
				Limit:  10,
				Offset: 0,
			},
			wantEventIDs: []string{
				"v1:query-event-e",
				"v1:query-event-d",
				"v1:query-event-c",
				"v1:query-event-b",
				"v1:query-event-a",
			},
			wantTotal: 5,
		},
		{
			name: "container filter",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					ContainerName: "container-a",
				},
				Limit: 10,
			},
			wantEventIDs: []string{
				"v1:query-event-e",
				"v1:query-event-d",
				"v1:query-event-b",
				"v1:query-event-a",
			},
			wantTotal: 4,
		},
		{
			name: "service filter",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					Service: "service-b",
				},
				Limit: 10,
			},
			wantEventIDs: []string{
				"v1:query-event-e",
				"v1:query-event-d",
				"v1:query-event-c",
			},
			wantTotal: 3,
		},
		{
			name: "level filter",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					Level: "ERROR",
				},
				Limit: 10,
			},
			wantEventIDs: []string{
				"v1:query-event-d",
				"v1:query-event-b",
			},
			wantTotal: 2,
		},
		{
			name: "time range includes both boundaries",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					Level: "ERROR",
					Start: &start,
					End:   &end,
				},
				Limit: 10,
			},
			wantEventIDs: []string{
				"v1:query-event-d",
				"v1:query-event-b",
			},
			wantTotal: 2,
		},
		{
			name: "combined filter",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					ContainerName: "container-a",
					Service:       "service-b",
					Level:         "ERROR",
					Start:         &start,
					End:           &end,
				},
				Limit: 10,
			},
			wantEventIDs: []string{
				"v1:query-event-d",
			},
			wantTotal: 1,
		},
		{
			name: "first page",
			query: model.LogListQuery{
				Limit:  2,
				Offset: 0,
			},
			wantEventIDs: []string{
				"v1:query-event-e",
				"v1:query-event-d",
			},
			wantTotal: 5,
		},
		{
			name: "second page keeps total before pagination",
			query: model.LogListQuery{
				Limit:  2,
				Offset: 2,
			},
			wantEventIDs: []string{
				"v1:query-event-c",
				"v1:query-event-b",
			},
			wantTotal: 5,
		},
		{
			name: "page beyond last returns empty slice",
			query: model.LogListQuery{
				Limit:  2,
				Offset: 10,
			},
			wantEventIDs: []string{},
			wantTotal:    5,
		},
		{
			name: "SQL-like input is treated as a value",
			query: model.LogListQuery{
				Filter: model.LogFilter{
					ContainerName: "' OR 1=1 --",
				},
				Limit: 10,
			},
			wantEventIDs: []string{},
			wantTotal:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, total, err := repo.List(
				context.Background(),
				tt.query,
			)
			if err != nil {
				t.Fatalf("List() returned an error: %v", err)
			}

			if logs == nil {
				t.Fatal("List() returned a nil slice")
			}

			if total != tt.wantTotal {
				t.Fatalf(
					"List() total = %d, want %d",
					total,
					tt.wantTotal,
				)
			}

			gotEventIDs := make([]string, 0, len(logs))
			for _, logEntry := range logs {
				gotEventIDs = append(
					gotEventIDs,
					logEntry.EventID,
				)

				if logEntry.RawEvent != nil {
					t.Fatalf(
						"list entry %q contains raw_event",
						logEntry.EventID,
					)
				}
			}

			if !equalStrings(gotEventIDs, tt.wantEventIDs) {
				t.Fatalf(
					"List() event IDs = %v, want %v",
					gotEventIDs,
					tt.wantEventIDs,
				)
			}
		})
	}

	var databaseCount int64
	if err := db.Model(&model.Log{}).
		Count(&databaseCount).
		Error; err != nil {
		t.Fatalf("count database records: %v", err)
	}

	if databaseCount != 5 {
		t.Fatalf(
			"query changed database count to %d, want 5",
			databaseCount,
		)
	}
}

func TestLogRepositoryFindByIDReturnsCompleteLog(
	t *testing.T,
) {
	repo, db := newLogQueryRepositoryForTest(t)
	seedLogQueryRecords(t, repo)

	var stored model.Log
	if err := db.Where(
		"event_id = ?",
		"v1:query-event-a",
	).Take(&stored).Error; err != nil {
		t.Fatalf("read seeded log ID: %v", err)
	}

	got, err := repo.FindByID(
		context.Background(),
		stored.ID,
	)
	if err != nil {
		t.Fatalf("FindByID() returned an error: %v", err)
	}

	if got.EventID != stored.EventID {
		t.Fatalf(
			"FindByID() EventID = %q, want %q",
			got.EventID,
			stored.EventID,
		)
	}

	if got.RawEvent == nil {
		t.Fatal("FindByID() omitted raw_event")
	}

	if got.Message != stored.Message {
		t.Fatalf(
			"FindByID() Message = %q, want %q",
			got.Message,
			stored.Message,
		)
	}

	if !got.LoggedAt.Equal(stored.LoggedAt) {
		t.Fatalf(
			"FindByID() LoggedAt = %v, want %v",
			got.LoggedAt,
			stored.LoggedAt,
		)
	}

	if got.SourceEventID == nil ||
		*got.SourceEventID != "source-query-event-a" {
		t.Fatalf(
			"FindByID() SourceEventID = %v, want %q",
			got.SourceEventID,
			"source-query-event-a",
		)
	}

	if *got.RawEvent != `{"sequence":1}` {
		t.Fatalf(
			"FindByID() RawEvent = %q, want %q",
			*got.RawEvent,
			`{"sequence":1}`,
		)
	}
}

func TestLogRepositoryFindByIDPreservesNotFound(
	t *testing.T,
) {
	repo, _ := newLogQueryRepositoryForTest(t)

	_, err := repo.FindByID(
		context.Background(),
		999,
	)
	if err == nil {
		t.Fatal("FindByID() returned nil error for missing log")
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf(
			"FindByID() error = %v, want gorm.ErrRecordNotFound",
			err,
		)
	}

	var notFound interface {
		NotFound() bool
	}
	if !errors.As(err, &notFound) ||
		!notFound.NotFound() {
		t.Fatalf(
			"FindByID() error = %v, want not-found marker",
			err,
		)
	}
}

func TestLogRepositoryListRejectsInvalidPagination(
	t *testing.T,
) {
	repo, _ := newLogQueryRepositoryForTest(t)

	tests := []struct {
		name  string
		query model.LogListQuery
	}{
		{
			name: "zero limit",
			query: model.LogListQuery{
				Limit: 0,
			},
		},
		{
			name: "negative limit",
			query: model.LogListQuery{
				Limit: -1,
			},
		},
		{
			name: "negative offset",
			query: model.LogListQuery{
				Limit:  20,
				Offset: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := repo.List(
				context.Background(),
				tt.query,
			); err == nil {
				t.Fatal("List() returned nil error")
			}
		})
	}
}

func TestLogRepositoryFindByIDRejectsNonPositiveID(
	t *testing.T,
) {
	repo, _ := newLogQueryRepositoryForTest(t)

	for _, id := range []int64{-1, 0} {
		if _, err := repo.FindByID(
			context.Background(),
			id,
		); err == nil {
			t.Fatalf(
				"FindByID(%d) returned nil error",
				id,
			)
		}
	}
}

func TestLogRepositoryListPreservesCanceledContext(
	t *testing.T,
) {
	repo, _ := newLogQueryRepositoryForTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := repo.List(
		ctx,
		model.LogListQuery{
			Limit: 20,
		},
	)
	if err == nil {
		t.Fatal("List() returned nil error for canceled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"List() error = %v, want context.Canceled",
			err,
		)
	}
}

func TestWrapLogReadErrorClassifiesTemporaryErrors(
	t *testing.T,
) {
	tests := []struct {
		name          string
		err           error
		wantTemporary bool
	}{
		{
			name: "SQLite busy",
			err: &fakeSQLiteError{
				code: sqlite3.SQLITE_BUSY,
			},
			wantTemporary: true,
		},
		{
			name:          "ordinary error",
			err:           errors.New("query failed"),
			wantTemporary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapLogReadError(
				"read logs",
				tt.err,
			)

			if !errors.Is(wrapped, tt.err) {
				t.Fatalf(
					"error = %v, want wrapped original error",
					wrapped,
				)
			}

			var temporary interface {
				Temporary() bool
			}
			isTemporary := errors.As(
				wrapped,
				&temporary,
			) && temporary.Temporary()

			if isTemporary != tt.wantTemporary {
				t.Fatalf(
					"temporary = %t, want %t",
					isTemporary,
					tt.wantTemporary,
				)
			}
		})
	}
}

func newLogQueryRepositoryForTest(
	t *testing.T,
) (*LogRepository, *gorm.DB) {
	t.Helper()

	db := openSQLiteForTest(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() returned an error: %v", err)
	}

	repo, err := NewLogRepository(db)
	if err != nil {
		t.Fatalf(
			"NewLogRepository() returned an error: %v",
			err,
		)
	}

	return repo, db
}

func seedLogQueryRecords(
	t *testing.T,
	repo *LogRepository,
) {
	t.Helper()

	rawEvent := `{"sequence":1}`
	sourceEventID := "source-query-event-a"
	records := []model.Log{
		newLogQueryTestRecord(
			"v1:query-event-a",
			"container-a",
			"service-a",
			"INFO",
			logQueryTestTime(0),
		),
		newLogQueryTestRecord(
			"v1:query-event-b",
			"container-a",
			"service-a",
			"ERROR",
			logQueryTestTime(1),
		),
		newLogQueryTestRecord(
			"v1:query-event-c",
			"container-b",
			"service-b",
			"WARN",
			logQueryTestTime(2),
		),
		newLogQueryTestRecord(
			"v1:query-event-d",
			"container-a",
			"service-b",
			"ERROR",
			logQueryTestTime(2),
		),
		newLogQueryTestRecord(
			"v1:query-event-e",
			"container-a",
			"service-b",
			"UNKNOWN",
			logQueryTestTime(3),
		),
	}
	records[0].RawEvent = &rawEvent
	records[0].SourceEventID = &sourceEventID

	inserted, err := repo.InsertBatch(
		context.Background(),
		records,
	)
	if err != nil {
		t.Fatalf("InsertBatch() returned an error: %v", err)
	}

	if inserted != int64(len(records)) {
		t.Fatalf(
			"InsertBatch() inserted %d, want %d",
			inserted,
			len(records),
		)
	}
}

func newLogQueryTestRecord(
	eventID string,
	containerName string,
	service string,
	level string,
	loggedAt time.Time,
) model.Log {
	return model.Log{
		EventID:       eventID,
		ContainerName: containerName,
		Service:       service,
		Level:         level,
		Message:       "message for " + eventID,
		Source:        "stdout",
		LoggedAt:      loggedAt,
		IngestedAt:    loggedAt.Add(time.Second),
	}
}

func logQueryTestTime(hourOffset int) time.Time {
	return time.Date(
		2026,
		time.July,
		24,
		10+hourOffset,
		0,
		0,
		0,
		time.UTC,
	)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}
