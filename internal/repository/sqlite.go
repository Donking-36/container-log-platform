package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	sqliteBusyTimeoutMillis = 5000
	sqliteMaxOpenConns      = 1
	sqliteMaxIdleConns      = 1
)

// OpenSQLite 打开SQLite数据库并配置基础运行参数。
func OpenSQLite(path string) (*gorm.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("SQLite database path must not be empty")
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf(
			"create SQLite database directory: %w",
			err,
		)
	}

	dsn := fmt.Sprintf(
		"%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)",
		path,
		sqliteBusyTimeoutMillis,
	)

	db, err := gorm.Open(
		sqlite.Open(dsn),
		&gorm.Config{
			// 运行日志由应用的slog链路统一输出。
			// 禁止GORM错误日志回显SQL参数中的日志正文。
			Logger: gormlogger.Default.LogMode(
				gormlogger.Silent,
			),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf(
			"access SQLite connection pool: %w",
			err,
		)
	}

	sqlDB.SetMaxOpenConns(sqliteMaxOpenConns)
	sqlDB.SetMaxIdleConns(sqliteMaxIdleConns)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}

	return db, nil
}
