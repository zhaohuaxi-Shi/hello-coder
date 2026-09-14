package store

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/hello-coder/hello-coder/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open ensures dataDir exists, creates the SQLite file if missing, then migrates tables.
func Open(dataDir string) (*gorm.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dsn := filepath.Join(dataDir, "hello-coder.db")
	created := false
	if _, err := os.Stat(dsn); os.IsNotExist(err) {
		created = true
		slog.Info("database not found, will create", "path", dsn)
	} else if err != nil {
		return nil, fmt.Errorf("stat database: %w", err)
	} else {
		slog.Info("database found", "path", dsn)
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.AutoMigrate(
		&model.User{},
		&model.KafkaConn{},
		&model.EsConn{},
		&model.RedisConn{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Drop legacy audit column if present.
	if db.Migrator().HasColumn(&model.KafkaConn{}, "created_by") {
		if err := db.Migrator().DropColumn(&model.KafkaConn{}, "created_by"); err != nil {
			return nil, fmt.Errorf("drop created_by: %w", err)
		}
		slog.Info("dropped legacy column kafka_conn.created_by")
	}

	if created {
		slog.Info("database created and migrated", "path", dsn)
	} else {
		slog.Info("database migrated", "path", dsn)
	}

	return db, nil
}
