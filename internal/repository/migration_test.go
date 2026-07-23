package repository

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
)

func TestMigrateCreatesExpectedSchema(t *testing.T) {
	db := openSQLiteForTest(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() returned an error: %v", err)
	}

	if !db.Migrator().HasTable(&model.Log{}) {
		t.Fatal("Migrate() did not create the logs table")
	}

	columnTypes, err := db.Migrator().ColumnTypes(&model.Log{})
	if err != nil {
		t.Fatalf("read logs table columns: %v", err)
	}

	wantColumns := []string{
		"id",
		"event_id",
		"source_event_id",
		"container_name",
		"container_id",
		"service",
		"level",
		"message",
		"source",
		"log_path",
		"log_offset",
		"logged_at",
		"ingested_at",
		"raw_event",
	}

	gotColumns := make(map[string]struct{}, len(columnTypes))
	for _, columnType := range columnTypes {
		gotColumns[columnType.Name()] = struct{}{}
	}

	if len(gotColumns) != len(wantColumns) {
		t.Fatalf(
			"logs table has %d columns, want %d: %v",
			len(gotColumns),
			len(wantColumns),
			gotColumns,
		)
	}

	for _, name := range wantColumns {
		if _, exists := gotColumns[name]; !exists {
			t.Errorf("logs table is missing column %q", name)
		}
	}

	indexTests := []struct {
		name        string
		wantColumns []string
	}{
		{
			name:        "uidx_logs_event_id",
			wantColumns: []string{"event_id"},
		},
		{
			name: "idx_logs_container_level_logged_at_id",
			wantColumns: []string{
				"container_name",
				"level",
				"logged_at",
				"id",
			},
		},
		{
			name: "idx_logs_service_logged_at_id",
			wantColumns: []string{
				"service",
				"logged_at",
				"id",
			},
		},
		{
			name: "idx_logs_logged_at_id",
			wantColumns: []string{
				"logged_at",
				"id",
			},
		},
	}

	for _, tt := range indexTests {
		t.Run(tt.name, func(t *testing.T) {
			if !db.Migrator().HasIndex(&model.Log{}, tt.name) {
				t.Fatalf("logs table is missing index %q", tt.name)
			}

			gotColumns := readIndexColumns(t, db, tt.name)

			if !slices.Equal(gotColumns, tt.wantColumns) {
				t.Fatalf(
					"index columns = %v, want %v",
					gotColumns,
					tt.wantColumns,
				)
			}
		})
	}
}

func TestMigrateCanRunRepeatedly(t *testing.T) {
	db := openSQLiteForTest(t)

	for attempt := 1; attempt <= 2; attempt++ {
		if err := Migrate(db); err != nil {
			t.Fatalf(
				"Migrate() attempt %d returned an error: %v",
				attempt,
				err,
			)
		}
	}
}

func TestMigrateRejectsNilDatabase(t *testing.T) {
	if err := Migrate(nil); err == nil {
		t.Fatal("Migrate(nil) returned nil error")
	}
}

func openSQLiteForTest(t *testing.T) *gorm.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")

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

	return db
}

func readIndexColumns(
	t *testing.T,
	db *gorm.DB,
	indexName string,
) []string {
	t.Helper()

	var rows []struct {
		Name string `gorm:"column:name"`
	}

	result := db.Raw(
		"SELECT name FROM pragma_index_info(?) ORDER BY seqno",
		indexName,
	).Scan(&rows)
	if result.Error != nil {
		t.Fatalf(
			"read columns of index %q: %v",
			indexName,
			result.Error,
		)
	}

	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, row.Name)
	}

	return columns
}
