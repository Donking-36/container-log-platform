package repository

import (
	"errors"
	"fmt"

	"github.com/Donking-36/container-log-platform/internal/model"
	"gorm.io/gorm"
)

// Migrate 创建或更新当前版本所需的数据库结构。
func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New(
			"migrate SQLite database: database must not be nil",
		)
	}

	if err := db.AutoMigrate(&model.Log{}); err != nil {
		return fmt.Errorf("migrate SQLite database: %w", err)
	}

	return nil
}
