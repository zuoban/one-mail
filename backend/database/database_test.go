package database

import (
	"errors"
	"path/filepath"
	"strings"
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

	var folderCols []indexCol
	if err := db.Raw("PRAGMA index_info('idx_folder_date')").Scan(&folderCols).Error; err != nil {
		t.Fatalf("failed to inspect idx_folder_date: %v", err)
	}
	if len(folderCols) < 3 || folderCols[0].Name != "folder" || folderCols[1].Name != "date" {
		t.Fatalf("expected idx_folder_date (folder, date, ...), got %+v", folderCols)
	}

	type qpRow struct {
		Detail string
	}
	var qp []qpRow
	if err := db.Raw(`EXPLAIN QUERY PLAN SELECT id FROM emails WHERE account_id IN (1, 2, 3) AND folder = 'INBOX' ORDER BY date DESC, id DESC LIMIT 50`).Scan(&qp).Error; err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed: %v", err)
	}
	for _, row := range qp {
		if row.Detail == "USE TEMP B-TREE FOR ORDER BY" {
			t.Fatalf("multi-account inbox query unexpectedly used temp B-tree sort: %+v", qp)
		}
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

		// Cache eviction physically removes the row and its FTS entry.
		if err := db.Delete(&email).Error; err != nil {
			t.Fatalf("cache eviction failed: %v", err)
		}
		if err := db.Raw(`SELECT count(*) FROM emails_fts WHERE emails_fts MATCH '"电子发票"'`).Scan(&count).Error; err != nil {
			t.Fatalf("FTS5 match after delete failed: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 FTS5 matches after eviction, got %d", count)
		}
	}
}

func ftsTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db := openSecurityMigrationDB(t)
	if err := db.Exec("CREATE VIRTUAL TABLE fts_probe USING fts5(value, tokenize='trigram')").Error; err != nil {
		if strings.Contains(err.Error(), "no such module") || strings.Contains(err.Error(), "no such tokenizer") {
			t.Skip("SQLite build does not include FTS5 trigram")
		}
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE fts_probe").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE emails(id integer PRIMARY KEY, subject text, "from" text, from_name text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO emails VALUES (1, 'first subject', 'first@example.com', 'First'), (2, 'second subject', NULL, NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	setupFTS5(db)
	if !IsFTS5Enabled() {
		t.Fatal("FTS5 setup failed despite available trigram support")
	}
	t.Cleanup(func() { fts5Enabled.Store(false) })
	return db
}

func TestFTSRepairsMissingStaleAndExtraRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"missing", "DELETE FROM emails_fts WHERE rowid = 2"},
		{"stale", "UPDATE emails_fts SET subject = 'stale content' WHERE rowid = 1"},
		{"extra", `INSERT INTO emails_fts(rowid, subject, "from", from_name) VALUES (999, 'orphan', '', '')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := ftsTestDatabase(t)
			if err := db.Exec(tc.sql).Error; err != nil {
				t.Fatal(err)
			}
			setupFTS5(db)
			if !IsFTS5Enabled() {
				t.Fatal("repaired index was not enabled")
			}
			var rows []struct {
				RowID    int
				Subject  string
				From     string
				FromName string
			}
			if err := db.Raw(`SELECT rowid AS row_id, subject, "from", from_name FROM emails_fts ORDER BY rowid`).Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[0].RowID != 1 || rows[0].Subject != "first subject" || rows[0].From != "first@example.com" || rows[0].FromName != "First" || rows[1].RowID != 2 || rows[1].Subject != "second subject" || rows[1].From != "" || rows[1].FromName != "" {
				t.Fatalf("FTS content was not restored: %+v", rows)
			}
			var count int
			if err := db.Raw(`SELECT count(*) FROM emails_fts WHERE emails_fts MATCH 'second'`).Scan(&count).Error; err != nil || count != 1 {
				t.Fatalf("repaired text is not searchable: %d, %v", count, err)
			}
		})
	}
}

func TestFTSConsistentStartupDoesNotRebuildAndUpgradesUpdateTrigger(t *testing.T) {
	db := ftsTestDatabase(t)
	if err := db.Exec("DROP TRIGGER emails_fts_au").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER emails_fts_au AFTER UPDATE OF subject, "from", from_name ON emails BEGIN
		DELETE FROM emails_fts WHERE rowid = OLD.id;
		INSERT INTO emails_fts(rowid, subject, "from", from_name)
		VALUES (NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, ''));
	END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register("test:reject_rebuild", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "DELETE FROM emails_fts") || strings.HasPrefix(tx.Statement.SQL.String(), "INSERT INTO emails_fts") {
			tx.AddError(errors.New("unexpected FTS rebuild"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	setupFTS5(db)
	if !IsFTS5Enabled() {
		t.Fatal("consistent startup attempted an index rebuild")
	}
	var before, after int64
	if err := db.Raw("SELECT total_changes()").Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE emails SET subject = subject, "from" = "from", from_name = from_name WHERE id = 1`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT total_changes()").Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after-before != 1 {
		t.Fatalf("unchanged indexed values rewrote the FTS index: %d changes", after-before)
	}
	if err := db.Exec("UPDATE emails SET subject = 'changed subject' WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Raw(`SELECT count(*) FROM emails_fts WHERE emails_fts MATCH 'changed'`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("changed subject did not update FTS: %d, %v", count, err)
	}
}

func TestFTSSetupFailuresDisableAndRollBack(t *testing.T) {
	for _, phase := range []string{"table", "trigger", "validation", "backfill"} {
		t.Run(phase, func(t *testing.T) {
			db := ftsTestDatabase(t)
			if err := db.Exec("DELETE FROM emails_fts WHERE rowid = 2").Error; err != nil {
				t.Fatal(err)
			}
			injected := false
			fail := func(tx *gorm.DB) {
				sql := tx.Statement.SQL.String()
				if (phase == "table" && strings.HasPrefix(sql, "CREATE VIRTUAL TABLE")) ||
					(phase == "trigger" && strings.HasPrefix(sql, "CREATE TRIGGER emails_fts_au")) ||
					(phase == "validation" && strings.HasPrefix(sql, "SELECT EXISTS")) ||
					(phase == "backfill" && strings.HasPrefix(sql, "INSERT INTO emails_fts")) {
					injected = true
					tx.AddError(errors.New("injected FTS setup failure"))
				}
			}
			if err := db.Callback().Raw().Before("gorm:raw").Register("test:fail_setup", fail); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Row().Before("gorm:row").Register("test:fail_validation", fail); err != nil {
				t.Fatal(err)
			}
			setupFTS5(db)
			if !injected || IsFTS5Enabled() {
				t.Fatalf("failed %s setup left FTS enabled or missed failure injection", phase)
			}
			var count int
			if err := db.Raw("SELECT count(*) FROM emails_fts").Scan(&count).Error; err != nil || count != 1 {
				t.Fatalf("failed setup did not roll back original index: %d, %v", count, err)
			}
		})
	}
}
