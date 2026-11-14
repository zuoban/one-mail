package database

import (
	"encoding/base64"
	"path/filepath"
	"testing"

	"one-mail/backend/config"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
)

func TestStartupPreservesMailCache(t *testing.T) {
	db := openTestDatabase(t)
	if err := InitializeSchema(db); err != nil {
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
	// Restarting preserves the user-selected cache window and downloaded bodies.
	for uid := uint(1); uid <= 200; uid++ {
		if err := db.Create(&models.Email{AccountID: account.ID, UID: uid, Folder: "INBOX", BodyFetched: true, BodyText: "cached body"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var schemaBefore, schemaAfter int
	if err := db.Raw("PRAGMA schema_version").Scan(&schemaBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := InitializeSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA schema_version").Scan(&schemaAfter).Error; err != nil {
		t.Fatal(err)
	}
	if schemaBefore != schemaAfter {
		t.Fatalf("startup changed schema: %d -> %d", schemaBefore, schemaAfter)
	}
	var count int64
	if err := db.Model(&models.Email{}).Count(&count).Error; err != nil || count != 200 {
		t.Fatalf("startup changed cache window: count %d, error %v", count, err)
	}
	if err := db.Model(&models.Email{}).Where("folder = ? AND body_fetched = ? AND body_text = ?", "INBOX", true, "cached body").Count(&count).Error; err != nil || count != 200 {
		t.Fatalf("startup changed cached bodies: count %d, error %v", count, err)
	}
}

func TestInitDatabaseRejectsWrongKey(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.Config{Database: config.DatabaseConfig{Type: "sqlite", Path: filepath.Join(t.TempDir(), "mail.db")}}
	t.Setenv("CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	db, err := InitDatabase()
	if err != nil || db == nil {
		t.Fatalf("InitDatabase returned %v, %v", db, err)
	}
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
		t.Fatal("wrong key must reject startup")
	}
}

func TestStartupRepairsFTS(t *testing.T) {
	db := openTestDatabase(t)
	if err := InitializeSchema(db); err != nil {
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
	if err := InitializeSchema(db); err != nil {
		t.Fatal(err)
	}
	if !IsFTS5Enabled() {
		t.Fatal("startup disabled a repairable FTS index")
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM emails_fts WHERE emails_fts MATCH 'repairable'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("startup did not repair FTS contents: %d, %v", count, err)
	}
	if err := db.Create(&models.Email{AccountID: account.ID, UID: 2, Folder: "INBOX", Subject: "trigger repaired"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM emails_fts WHERE emails_fts MATCH 'trigger'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("startup did not repair the missing FTS trigger: %d, %v", count, err)
	}
}
