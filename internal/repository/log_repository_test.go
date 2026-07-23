package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

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
