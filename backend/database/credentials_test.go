package database

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")+"?_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestStartupRejectsUnencryptedCredentials(t *testing.T) {
	for _, kind := range []string{"account", "deleted_account", "telegram"} {
		t.Run(kind, func(t *testing.T) {
			db := openTestDatabase(t)
			if err := InitializeSchema(db); err != nil {
				t.Fatal(err)
			}
			const plaintext = "unprotected-credential"
			var stored string
			if kind == "telegram" {
				cfg := models.TelegramConfig{ID: 1, BotToken: plaintext, ChatID: "chat"}
				if err := db.Create(&cfg).Error; err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := db.Model(&cfg).Select("bot_token").Scan(&stored).Error; err != nil || stored != plaintext {
						t.Fatalf("rejected startup changed token: %q, %v", stored, err)
					}
				}()
			} else {
				account := models.EmailAccount{Email: "mail@example.com", Password: plaintext}
				if err := db.Create(&account).Error; err != nil {
					t.Fatal(err)
				}
				if kind == "deleted_account" {
					if err := db.Delete(&account).Error; err != nil {
						t.Fatal(err)
					}
				}
				defer func() {
					if err := db.Unscoped().Model(&account).Select("password").Scan(&stored).Error; err != nil || stored != plaintext {
						t.Fatalf("rejected startup changed password: %q, %v", stored, err)
					}
				}()
			}
			if err := InitializeSchema(db); err == nil || !strings.Contains(err.Error(), "not encrypted") || strings.Contains(err.Error(), plaintext) {
				t.Fatalf("startup must reject plaintext without exposing it: %v", err)
			}
		})
	}
}
