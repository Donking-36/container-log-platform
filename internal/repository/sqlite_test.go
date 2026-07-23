package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSQLite(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"data",
		"test.db",
	)

	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite() returned an error: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() returned an error: %v", err)
	}

	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close SQLite database: %v", err)
		}
	})

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping SQLite database: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("SQLite database file was not created: %v", err)
	}

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Row().
		Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}

	if journalMode != "wal" {
		t.Fatalf(
			"journal_mode = %q, want %q",
			journalMode,
			"wal",
		)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Row().
		Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}

	if busyTimeout != sqliteBusyTimeoutMillis {
		t.Fatalf(
			"busy_timeout = %d, want %d",
			busyTimeout,
			sqliteBusyTimeoutMillis,
		)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Row().
		Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}

	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf(
			"maximum open connections = %d, want 1",
			got,
		)
	}
}

func TestOpenSQLiteRejectsEmptyPath(t *testing.T) {
	if _, err := OpenSQLite("   "); err == nil {
		t.Fatal("OpenSQLite() returned nil error for empty path")
	}
}
