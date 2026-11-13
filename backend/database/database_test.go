package database

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"one-mail/backend/internal/models"
)

func TestSetupSQLiteAndMigrateIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := SetupSQLite(db, true); err != nil {
		t.Fatalf("SetupSQLite failed: %v", err)
	}

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode;").Scan(&journalMode).Error; err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal_mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout;").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("failed to query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if stats := sqlDB.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected MaxOpenConnections=1, got %d", stats.MaxOpenConnections)
	}

	// Simulate legacy schema with misaligned idx_account_folder_date(account_id, date, folder)
	if err := db.AutoMigrate(&models.Email{}); err != nil {
		t.Fatalf("initial AutoMigrate failed: %v", err)
	}
	_ = db.Exec("DROP INDEX IF EXISTS idx_account_folder_date").Error
	if err := db.Exec("CREATE INDEX idx_account_folder_date ON emails(account_id, date, folder)").Error; err != nil {
		t.Fatalf("failed to create legacy index: %v", err)
	}

	if err := MigrateAndOptimize(db); err != nil {
		t.Fatalf("MigrateAndOptimize failed: %v", err)
	}

	type indexCol struct {
		Seqno int
		Cid   int
		Name  string
	}
	var cols []indexCol
	if err := db.Raw("PRAGMA index_info('idx_account_folder_date')").Scan(&cols).Error; err != nil {
		t.Fatalf("failed to inspect rebuilt index: %v", err)
	}
	if len(cols) < 3 || cols[0].Name != "account_id" || cols[1].Name != "folder" || cols[2].Name != "date" {
		t.Fatalf("expected rebuilt index (account_id, folder, date, ...), got %+v", cols)
	}

	// Verify FTS5 trigger sync when FTS5 is compiled in
	if IsFTS5Enabled() {
		email := models.Email{
			AccountID: 1,
			MessageID: "msg-fts-1",
			From:      "billing@example.com",
			FromName:  "财务账单中心",
			To:        "me@example.com",
			Subject:   "2026年电子发票通知",
			Date:      time.Now(),
			Folder:    "INBOX",
			UID:       101,
		}
		if err := db.Create(&email).Error; err != nil {
			t.Fatalf("failed to create email: %v", err)
		}

		var count int64
		if err := db.Raw(`SELECT count(*) FROM emails_fts WHERE emails_fts MATCH '"电子发票"'`).Scan(&count).Error; err != nil {
			t.Fatalf("FTS5 match failed: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 FTS5 match, got %d", count)
		}

		// Soft-delete should remove row from FTS5 index via update trigger
		if err := db.Delete(&email).Error; err != nil {
			t.Fatalf("soft delete failed: %v", err)
		}
		if err := db.Raw(`SELECT count(*) FROM emails_fts WHERE emails_fts MATCH '"电子发票"'`).Scan(&count).Error; err != nil {
			t.Fatalf("FTS5 match after delete failed: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 FTS5 matches after soft delete, got %d", count)
		}
	}
}
