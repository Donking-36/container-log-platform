package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
	sqlite3 "modernc.org/sqlite/lib"
)

type fakeSQLiteError struct {
	code int
}

func (e *fakeSQLiteError) Error() string {
	return fmt.Sprintf("SQLite error code %d", e.code)
}

func (e *fakeSQLiteError) Code() int {
	return e.code
}

func TestLogRepositoryInsertBatchIsIdempotent(t *testing.T) {
	db := openSQLiteForTest(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() returned an error: %v", err)
	}

	repo, err := NewLogRepository(db)
	if err != nil {
		t.Fatalf("NewLogRepository() returned an error: %v", err)
	}

	logs := []model.Log{
		newTestLog("v1:event-001"),
		newTestLog("v1:event-001"),
		newTestLog("v1:event-002"),
	}

	for attempt := 1; attempt <= 10; attempt++ {
		inserted, err := repo.InsertBatch(
			context.Background(),
			logs,
		)
		if err != nil {
			t.Fatalf(
				"InsertBatch() attempt %d returned an error: %v",
				attempt,
				err,
			)
		}

		wantInserted := int64(0)
		if attempt == 1 {
			wantInserted = 2
		}

		if inserted != wantInserted {
			t.Fatalf(
				"attempt %d inserted %d logs, want %d",
				attempt,
				inserted,
				wantInserted,
			)
		}
	}

	var count int64
	result := db.Model(&model.Log{}).Count(&count)
	if result.Error != nil {
		t.Fatalf("count persisted logs: %v", result.Error)
	}

	if count != 2 {
		t.Fatalf("persisted log count = %d, want 2", count)
	}

	for index, entry := range logs {
		if entry.ID != 0 {
			t.Fatalf(
				"input log %d ID was changed to %d",
				index,
				entry.ID,
			)
		}
	}
}

func TestLogRepositoryPersistsAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"data",
		"persistent.db",
	)

	entry := newTestLog("v1:persistent-event")

	firstDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("first OpenSQLite() returned an error: %v", err)
	}

	if err := Migrate(firstDB); err != nil {
		t.Fatalf("first Migrate() returned an error: %v", err)
	}

	firstRepo, err := NewLogRepository(firstDB)
	if err != nil {
		t.Fatalf("first NewLogRepository() error: %v", err)
	}

	inserted, err := firstRepo.InsertBatch(
		context.Background(),
		[]model.Log{entry},
	)
	if err != nil {
		t.Fatalf("first InsertBatch() returned an error: %v", err)
	}

	if inserted != 1 {
		t.Fatalf("first InsertBatch() inserted %d, want 1", inserted)
	}

	firstSQLDB, err := firstDB.DB()
	if err != nil {
		t.Fatalf("access first connection pool: %v", err)
	}

	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first database connection: %v", err)
	}

	secondDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("second OpenSQLite() returned an error: %v", err)
	}

	secondSQLDB, err := secondDB.DB()
	if err != nil {
		t.Fatalf("access second connection pool: %v", err)
	}

	t.Cleanup(func() {
		if err := secondSQLDB.Close(); err != nil {
			t.Errorf("close second database connection: %v", err)
		}
	})

	if err := Migrate(secondDB); err != nil {
		t.Fatalf("second Migrate() returned an error: %v", err)
	}

	secondRepo, err := NewLogRepository(secondDB)
	if err != nil {
		t.Fatalf("second NewLogRepository() error: %v", err)
	}

	inserted, err = secondRepo.InsertBatch(
		context.Background(),
		[]model.Log{entry},
	)
	if err != nil {
		t.Fatalf("second InsertBatch() returned an error: %v", err)
	}

	if inserted != 0 {
		t.Fatalf(
			"duplicate insert after reopen inserted %d, want 0",
			inserted,
		)
	}

	var stored model.Log
	result := secondDB.
		Where("event_id = ?", entry.EventID).
		First(&stored)
	if result.Error != nil {
		t.Fatalf("read persisted log: %v", result.Error)
	}

	if stored.Message != entry.Message {
		t.Fatalf(
			"stored message = %q, want %q",
			stored.Message,
			entry.Message,
		)
	}
}

func TestLogRepositoryInsertBatchAcceptsEmptyBatch(
	t *testing.T,
) {
	db := openSQLiteForTest(t)

	repo, err := NewLogRepository(db)
	if err != nil {
		t.Fatalf("NewLogRepository() returned an error: %v", err)
	}

	inserted, err := repo.InsertBatch(
		context.Background(),
		nil,
	)
	if err != nil {
		t.Fatalf("InsertBatch(nil) returned an error: %v", err)
	}

	if inserted != 0 {
		t.Fatalf("InsertBatch(nil) inserted %d, want 0", inserted)
	}
}

func TestWrapLogInsertErrorClassifiesTemporaryErrors(
	t *testing.T,
) {
	tests := []struct {
		name          string
		err           error
		wantTemporary bool
	}{
		{
			name: "busy",
			err: &fakeSQLiteError{
				code: sqlite3.SQLITE_BUSY,
			},
			wantTemporary: true,
		},
		{
			name: "extended busy code",
			err: &fakeSQLiteError{
				code: sqlite3.SQLITE_BUSY | (1 << 8),
			},
			wantTemporary: true,
		},
		{
			name: "locked",
			err: &fakeSQLiteError{
				code: sqlite3.SQLITE_LOCKED,
			},
			wantTemporary: true,
		},
		{
			name: "constraint violation",
			err: &fakeSQLiteError{
				code: sqlite3.SQLITE_CONSTRAINT,
			},
			wantTemporary: false,
		},
		{
			name:          "non-SQLite error",
			err:           errors.New("unexpected database error"),
			wantTemporary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapLogInsertError(tt.err)

			if !errors.Is(wrapped, tt.err) {
				t.Fatalf(
					"error = %v, want wrapped original error",
					wrapped,
				)
			}

			var temporary interface {
				Temporary() bool
			}
			isTemporary := errors.As(wrapped, &temporary) &&
				temporary.Temporary()

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

func TestLogRepositoryClassifiesLockedDatabaseAsTemporary(
	t *testing.T,
) {
	path := filepath.Join(t.TempDir(), "locked.db")

	firstDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open first database: %v", err)
	}
	firstSQLDB, err := firstDB.DB()
	if err != nil {
		t.Fatalf("access first connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := firstSQLDB.Close(); err != nil {
			t.Errorf("close first database: %v", err)
		}
	})

	if err := Migrate(firstDB); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	secondDB, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open second database: %v", err)
	}
	secondSQLDB, err := secondDB.DB()
	if err != nil {
		t.Fatalf("access second connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := secondSQLDB.Close(); err != nil {
			t.Errorf("close second database: %v", err)
		}
	})

	if result := secondDB.Exec(
		"PRAGMA busy_timeout = 1",
	); result.Error != nil {
		t.Fatalf("set test busy timeout: %v", result.Error)
	}

	transaction := firstDB.Begin()
	if transaction.Error != nil {
		t.Fatalf("begin write transaction: %v", transaction.Error)
	}
	defer func() {
		_ = transaction.Rollback().Error
	}()

	if result := transaction.Create(
		&[]model.Log{newTestLog("v1:lock-holder")},
	); result.Error != nil {
		t.Fatalf("acquire SQLite write lock: %v", result.Error)
	}

	repo, err := NewLogRepository(secondDB)
	if err != nil {
		t.Fatalf("create second repository: %v", err)
	}

	_, err = repo.InsertBatch(
		context.Background(),
		[]model.Log{newTestLog("v1:blocked-writer")},
	)
	if err == nil {
		t.Fatal("expected locked database error")
	}

	var temporary interface {
		Temporary() bool
	}
	if !errors.As(err, &temporary) ||
		!temporary.Temporary() {
		t.Fatalf(
			"error = %v, want temporary storage error",
			err,
		)
	}
}

func newTestLog(eventID string) model.Log {
	loggedAt := time.Date(
		2026,
		time.July,
		23,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	return model.Log{
		EventID:       eventID,
		ContainerName: "log-producer-1",
		Service:       "log-producer",
		Level:         "INFO",
		Message:       "test message for " + eventID,
		Source:        "stdout",
		LoggedAt:      loggedAt,
		IngestedAt:    loggedAt.Add(time.Second),
	}
}
