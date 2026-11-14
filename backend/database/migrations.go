package database

import (
	"fmt"

	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

type migration struct {
	version int
	name    string
	apply   func(*gorm.DB) error
}

// Add schema/data changes here rather than performing AutoMigrate on startup.
// Unversioned databases run the same conditional upgrade path as a new database.
var migrations = []migration{
	{1, "personal_recent_mail_cache", migratePersonalDatabase},
	{2, "encrypted_credentials", migrateCredentials},
	{4, "notification_outbox", func(tx *gorm.DB) error {
		return tx.AutoMigrate(&models.NotificationTask{})
	}},
}

// MigrateAndOptimize applies each historical migration once, then validates
// credentials and repairs the optional FTS index on every startup.
func MigrateAndOptimize(db *gorm.DB) error {
	fts5Enabled.Store(false)
	if err := validateCredentials(db); err != nil {
		return err
	}
	if err := db.Connection(func(conn *gorm.DB) (result error) {
		// PRAGMA foreign_keys is connection scoped and cannot change inside a
		// transaction. Keep all table replacements and restoration pinned.
		conn = conn.Session(&gorm.Session{NewDB: true})
		var foreignKeys int
		if err := conn.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
			return err
		}
		if foreignKeys != 0 {
			if err := conn.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
				return err
			}
			defer func() {
				if err := conn.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
					if result == nil {
						result = fmt.Errorf("restore foreign keys: %w", err)
					} else {
						result = fmt.Errorf("%w; restore foreign keys: %v", result, err)
					}
				}
			}()
		}
		if err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			version integer PRIMARY KEY, name text NOT NULL, applied_at datetime NOT NULL
		)`).Error; err != nil {
			return err
		}
		for _, step := range migrations {
			if err := applyMigration(conn, step); err != nil {
				return fmt.Errorf("migration %d (%s): %w", step.version, step.name, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to migrate personal database: %w", err)
	}
	setupFTS5(db)
	return nil
}

func applyMigration(db *gorm.DB, step migration) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var applied int64
		if err := tx.Table("schema_migrations").Where("version = ?", step.version).Count(&applied).Error; err != nil {
			return err
		}
		if applied != 0 {
			return nil
		}
		// Table cutovers can reset AUTOINCREMENT high-water marks. Preserve
		// every surviving table's reservation, even if its highest row was deleted.
		var sequences []struct {
			Name string
			Seq  int64
		}
		if tx.Migrator().HasTable("sqlite_sequence") {
			if err := tx.Raw("SELECT name, MAX(seq) AS seq FROM sqlite_sequence GROUP BY name").Scan(&sequences).Error; err != nil {
				return err
			}
		}
		if err := step.apply(tx); err != nil {
			return err
		}
		for _, sequence := range sequences {
			if !tx.Migrator().HasTable(sequence.Name) {
				continue
			}
			if err := tx.Exec("INSERT INTO sqlite_sequence(name, seq) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = ?)", sequence.Name, sequence.Seq, sequence.Name).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE sqlite_sequence SET seq = MAX(COALESCE(seq, 0), ?) WHERE name = ?", sequence.Seq, sequence.Name).Error; err != nil {
				return err
			}
		}
		var violations []struct {
			Table  string
			RowID  *int64
			Parent string
		}
		if err := tx.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
			return err
		}
		if len(violations) != 0 {
			return fmt.Errorf("foreign key violation in %s referencing %s", violations[0].Table, violations[0].Parent)
		}
		return tx.Exec("INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, CURRENT_TIMESTAMP)", step.version, step.name).Error
	})
}

func migratePersonalDatabase(tx *gorm.DB) error {
	if err := migratePersonalSchema(tx); err != nil {
		return err
	}
	if err := migratePersonalModels(tx); err != nil {
		return err
	}
	if err := mergeTelegramConfig(tx); err != nil {
		return err
	}
	if err := migrateRecentMailCache(tx); err != nil {
		return err
	}
	return ensureEmailIndexes(tx)
}
