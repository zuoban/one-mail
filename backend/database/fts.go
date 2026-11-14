package database

import (
	"fmt"
	"log"
	"sync/atomic"

	"gorm.io/gorm"
)

var fts5Enabled atomic.Bool

func IsFTS5Enabled() bool {
	return fts5Enabled.Load()
}

func setupFTS5(db *gorm.DB) {
	fts5Enabled.Store(false)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS emails_fts USING fts5(
			subject, "from", from_name, tokenize='trigram'
		)`).Error; err != nil {
			return err
		}
		triggers := []string{
			`CREATE TRIGGER IF NOT EXISTS emails_fts_ai AFTER INSERT ON emails BEGIN
				INSERT INTO emails_fts(rowid, subject, "from", from_name)
				VALUES (NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, ''));
			END;`,
			`CREATE TRIGGER IF NOT EXISTS emails_fts_ad AFTER DELETE ON emails BEGIN
				DELETE FROM emails_fts WHERE rowid = OLD.id;
			END;`,
			`CREATE TRIGGER IF NOT EXISTS emails_fts_au AFTER UPDATE OF subject, "from", from_name ON emails
			WHEN OLD.subject IS NOT NEW.subject OR OLD."from" IS NOT NEW."from" OR OLD.from_name IS NOT NEW.from_name
			BEGIN
				DELETE FROM emails_fts WHERE rowid = OLD.id;
				INSERT INTO emails_fts(rowid, subject, "from", from_name)
				VALUES (NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, ''));
			END;`,
		}
		for _, trigger := range triggers {
			if err := tx.Exec(trigger).Error; err != nil {
				return err
			}
		}
		// Compare normalized indexed content as well as missing and extra rowids.
		const inconsistentSQL = `SELECT EXISTS (
			SELECT 1 FROM emails e LEFT JOIN emails_fts f ON f.rowid = e.id
			WHERE f.rowid IS NULL OR f.subject IS NOT COALESCE(e.subject, '')
				OR f."from" IS NOT COALESCE(e."from", '') OR f.from_name IS NOT COALESCE(e.from_name, '')
		) OR EXISTS (
			SELECT 1 FROM emails_fts f LEFT JOIN emails e ON e.id = f.rowid WHERE e.id IS NULL
		)`
		var inconsistent bool
		if err := tx.Raw(inconsistentSQL).Scan(&inconsistent).Error; err != nil {
			return err
		}
		if inconsistent {
			if err := tx.Exec("DELETE FROM emails_fts").Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO emails_fts(rowid, subject, "from", from_name)
				SELECT id, COALESCE(subject, ''), COALESCE("from", ''), COALESCE(from_name, '') FROM emails`).Error; err != nil {
				return err
			}
			if err := tx.Raw(inconsistentSQL).Scan(&inconsistent).Error; err != nil {
				return err
			}
			if inconsistent {
				return fmt.Errorf("FTS5 content remains inconsistent after repair")
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("SQLite FTS5 setup failed (%v); falling back to indexed LIKE search", err)
		return
	}
	fts5Enabled.Store(true)
}
