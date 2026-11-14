package database

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"one-mail/backend/config"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

func assertMigrationVersions(t *testing.T, db *gorm.DB) {
	t.Helper()
	var versions []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != len(migrations) {
		t.Fatalf("got migration versions %v, want %d records", versions, len(migrations))
	}
	for i, step := range migrations {
		if versions[i] != step.version {
			t.Fatalf("migration version %d missing from %v", step.version, versions)
		}
	}
	if !db.Migrator().HasTable(&models.NotificationTask{}) {
		t.Fatal("notification outbox table was not created")
	}
}

func TestVersionedMigrationBootstrapsEmptyLegacyAndCurrentDatabases(t *testing.T) {
	for _, fixture := range []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{"empty", openSecurityMigrationDB},
		{"legacy", legacyPersonalDatabase},
		{"current_without_versions", securityMigrationDB},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db := fixture.open(t)
			if err := MigrateAndOptimize(db); err != nil {
				t.Fatal(err)
			}
			assertMigrationVersions(t, db)
			assertPersonalSchema(t, db)
			if err := MigrateAndOptimize(db); err != nil {
				t.Fatal(err)
			}
			assertMigrationVersions(t, db)
		})
	}
}

func TestVersionedStartupDoesNotRepeatSchemaOrCacheMigrations(t *testing.T) {
	db := openSecurityMigrationDB(t)
	if err := MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	sealed, err := credentials.Encrypt("stored-password")
	if err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{Email: "mail@example.com", Password: sealed}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	// A configured 200-message window must survive startup after the
	// historical 50-message cleanup migration has been recorded.
	for uid := uint(1); uid <= 200; uid++ {
		if err := db.Create(&models.Email{AccountID: account.ID, UID: uid, Folder: "INBOX", BodyFetched: true, BodyText: "cached body"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.Email{AccountID: account.ID, UID: 100, Folder: "Archive"}).Error; err != nil {
		t.Fatal(err)
	}
	var schemaBefore, schemaAfter int
	if err := db.Raw("PRAGMA schema_version").Scan(&schemaBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register("test:reject_historical_migration", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "DELETE FROM emails WHERE") || strings.HasPrefix(sql, "ALTER TABLE") || strings.HasPrefix(sql, "DROP ") {
			tx.AddError(errors.New("historical migration repeated"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA schema_version").Scan(&schemaAfter).Error; err != nil {
		t.Fatal(err)
	}
	if schemaBefore != schemaAfter {
		t.Fatalf("versioned startup changed schema: %d -> %d", schemaBefore, schemaAfter)
	}
	var count int64
	if err := db.Model(&models.Email{}).Count(&count).Error; err != nil || count != 201 {
		t.Fatalf("versioned startup repeated cache pruning: count %d, error %v", count, err)
	}
	if err := db.Model(&models.Email{}).Where("folder = ? AND body_fetched = ? AND body_text = ?", "INBOX", true, "cached body").Count(&count).Error; err != nil || count != 200 {
		t.Fatalf("versioned startup changed cached bodies: count %d, error %v", count, err)
	}
	assertMigrationVersions(t, db)
}

func TestFailedMigrationRollsBackSchemaDataAndVersion(t *testing.T) {
	for _, failure := range []string{"apply", "record"} {
		t.Run(failure, func(t *testing.T) {
			db := openSecurityMigrationDB(t)
			if err := MigrateAndOptimize(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TABLE migration_fixture(value text)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("INSERT INTO migration_fixture VALUES ('original')").Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected migration failure")
			if failure == "record" {
				if err := db.Callback().Raw().Before("gorm:raw").Register("test:reject_migration_record", func(tx *gorm.DB) {
					if strings.HasPrefix(tx.Statement.SQL.String(), "INSERT INTO schema_migrations") {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			step := migration{version: migrations[len(migrations)-1].version + 1, name: "failure_fixture", apply: func(tx *gorm.DB) error {
				if err := tx.Exec("CREATE TABLE rollback_fixture(value text)").Error; err != nil {
					return err
				}
				if err := tx.Exec("UPDATE migration_fixture SET value = 'changed'").Error; err != nil {
					return err
				}
				if failure == "apply" {
					return injected
				}
				return nil
			}}
			if err := applyMigration(db, step); !errors.Is(err, injected) {
				t.Fatalf("got %v, want injected failure", err)
			}
			if db.Migrator().HasTable("rollback_fixture") {
				t.Fatal("failed migration retained schema changes")
			}
			var value string
			if err := db.Raw("SELECT value FROM migration_fixture").Scan(&value).Error; err != nil || value != "original" {
				t.Fatalf("failed migration retained data changes: %q, %v", value, err)
			}
			assertMigrationVersions(t, db)
		})
	}
}

func TestInitDatabaseReturnsDependencyAndRejectsWrongKeyAfterVersioning(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.Config{Database: config.DatabaseConfig{Type: "sqlite", Path: filepath.Join(t.TempDir(), "mail.db")}}
	t.Setenv("CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	db, err := InitDatabase()
	if err != nil || db == nil {
		t.Fatalf("InitDatabase returned %v, %v", db, err)
	}
	assertMigrationVersions(t, db)
	sealed, err := credentials.Encrypt("deleted-account-password")
	if err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{Email: "deleted@example.com", Password: sealed}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&account).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	wrongKey := make([]byte, 32)
	wrongKey[0] = 1
	t.Setenv("CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(wrongKey))
	if opened, err := InitDatabase(); err == nil || opened != nil {
		t.Fatal("wrong key must reject startup after all migrations are recorded")
	}
}

func TestVersionedStartupStillRepairsFTS(t *testing.T) {
	db := openSecurityMigrationDB(t)
	if err := MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	if !IsFTS5Enabled() {
		t.Skip("SQLite was built without FTS5 trigram support")
	}
	sealed, err := credentials.Encrypt("mail-password")
	if err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{Email: "mail@example.com", Password: sealed}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, UID: 1, Folder: "INBOX", Subject: "repairable message"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM emails_fts").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TRIGGER emails_fts_ai").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	if !IsFTS5Enabled() {
		t.Fatal("versioned startup disabled a repairable FTS index")
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM emails_fts WHERE emails_fts MATCH 'repairable'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("versioned startup did not repair FTS contents: %d, %v", count, err)
	}
	if err := db.Create(&models.Email{AccountID: account.ID, UID: 2, Folder: "INBOX", Subject: "trigger repaired"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM emails_fts WHERE emails_fts MATCH 'trigger'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("versioned startup did not repair the missing FTS trigger: %d, %v", count, err)
	}
	assertMigrationVersions(t, db)
}
