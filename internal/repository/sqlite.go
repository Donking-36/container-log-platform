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
	// busy_timeout 让短暂锁竞争先在驱动层等待，减少无意义的立即失败。
	sqliteBusyTimeoutMillis = 5000
	// SQLite 只保留一个共享连接，避免连接池内多个写连接互相争锁。
	sqliteMaxOpenConns = 1
	sqliteMaxIdleConns = 1
)

// OpenSQLite 打开 SQLite 数据库并配置基础运行参数。
// WAL 允许读取与写入更好地并行，busy_timeout 缓冲短暂锁竞争，
// foreign_keys 确保后续模型新增关系时不会绕过引用完整性。
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

	// 这些 pragma 放在 DSN 中，确保建连时即生效。
	dsn := fmt.Sprintf(
		"%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)",
		path,
		sqliteBusyTimeoutMillis,
	)

	db, err := gorm.Open(
		sqlite.Open(dsn),
		&gorm.Config{
			// 运行日志由应用的 slog 链路统一输出。
			// 禁止 GORM 错误日志回显 SQL 参数中的日志正文。
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

	// 单连接是当前 MVP 的有意取舍：牺牲写并行度，换取 SQLite 下更可预测
	// 的事务和锁行为。扩展吞吐量时应更换存储方案，而非盲目增大连接池。
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConns)
	sqlDB.SetMaxIdleConns(sqliteMaxIdleConns)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}

	return db, nil
}
