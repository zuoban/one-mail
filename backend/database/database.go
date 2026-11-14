package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"one-mail/backend/config"
	"one-mail/backend/internal/credentials"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase() (*gorm.DB, error) {
	fts5Enabled.Store(false)
	dsn := config.GetDSN()
	if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if err := credentials.Init(os.Getenv("CREDENTIALS_KEY"), dsn+".key"); err != nil {
		return nil, fmt.Errorf("initialize credential encryption: %w", err)
	}

	// Keep slow-query warnings in release builds; parameters remain redacted.
	logLevel := logger.Warn
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
			ParameterizedQueries:      true,
		},
	)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database connection: %w", err)
	}
	if err := SetupSQLite(db, config.AppConfig != nil && config.AppConfig.Database.Type == "sqlite"); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := MigrateAndOptimize(db); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	log.Println("Database connected and migrated successfully!")
	return db, nil
}

// SetupSQLite configures WAL mode, busy_timeout, memory/cache PRAGMAs, and single-writer connection pooling.
func SetupSQLite(db *gorm.DB, isSQLite bool) error {
	if !isSQLite || db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	// Serialize writes/reads on a single pooled connection to eliminate SQLITE_BUSY contention
	// and ensure connection-scoped PRAGMAs persist across all queries.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -16000;",
	}
	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			return fmt.Errorf("failed to apply %s: %w", pragma, err)
		}
	}

	return nil
}
