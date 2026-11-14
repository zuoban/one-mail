package database

import (
	"encoding/base64"
	"path/filepath"
	"testing"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func securityMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCredentialMigrationIncludesDeletedAccountsAndRejectsWrongKey(t *testing.T) {
	db := securityMigrationDB(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	accounts := []models.EmailAccount{{Email: "first@example.com", Password: "first-secret"}, {Email: "deleted@example.com", Password: "deleted-secret"}}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&accounts[1]).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateCredentials(db); err != nil {
		t.Fatal(err)
	}
	var stored []models.EmailAccount
	if err := db.Unscoped().Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	for i, row := range stored {
		if !credentials.IsEncrypted(row.Password) {
			t.Fatal("plaintext credential remains in database")
		}
		plain, err := credentials.Decrypt(row.Password)
		if err != nil || plain != accounts[i].Password {
			t.Fatalf("credential changed during migration: %v", err)
		}
	}
	if err := migrateCredentials(db); err != nil {
		t.Fatal("migration must be repeatable", err)
	}
	other := make([]byte, 32)
	other[0] = 1
	if err := credentials.Init(base64.StdEncoding.EncodeToString(other), ""); err != nil {
		t.Fatal(err)
	}
	if err := migrateCredentials(db); err == nil {
		t.Fatal("wrong key must stop startup rather than overwrite credentials")
	}
}

func TestLegacyTelegramConfigMigratesOnlyForUnambiguousOwner(t *testing.T) {
	for _, userCount := range []int{1, 2} {
		t.Run(string(rune('0'+userCount))+"_users", func(t *testing.T) {
			db := securityMigrationDB(t)
			users := []models.User{{Username: "first"}, {Username: "second"}}
			if err := db.Create(users[:userCount]).Error; err != nil {
				t.Fatal(err)
			}
			legacy := models.TelegramConfig{Enabled: true, BotToken: "legacy-token", ChatID: "legacy-chat"}
			if err := db.Create(&legacy).Error; err != nil {
				t.Fatal(err)
			}
			if err := migrateUserSettings(db); err != nil {
				t.Fatal(err)
			}
			var got models.TelegramConfig
			if err := db.First(&got, legacy.ID).Error; err != nil {
				t.Fatal(err)
			}
			if userCount == 1 {
				if got.UserID != users[0].ID || !got.Enabled {
					t.Fatal("single-user legacy configuration was not preserved")
				}
			} else if got.UserID != 0 || got.Enabled {
				t.Fatal("ambiguous configuration must be disabled and unassigned")
			}
			if err := migrateUserSettings(db); err != nil {
				t.Fatal("repeat migration failed", err)
			}
		})
	}
}
