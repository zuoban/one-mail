package database

import (
	"encoding/base64"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openSecurityMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")+"?_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

func securityMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openSecurityMigrationDB(t)
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

func TestPersonalTelegramConfigMerge(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	for _, tc := range []struct {
		name    string
		configs []models.TelegramConfig
		want    models.TelegramConfig
	}{
		{name: "empty"},
		{
			name: "prefer_latest_complete_with_id_tiebreak",
			configs: []models.TelegramConfig{
				{ID: 1, UpdatedAt: older, Enabled: true, BotToken: "first", ChatID: "first-chat"},
				{ID: 4, UpdatedAt: newer, Enabled: true, BotToken: "second", ChatID: "second-chat"},
				{ID: 5, UpdatedAt: newer, Enabled: false, BotToken: "chosen", ChatID: "chosen-chat"},
				{ID: 6, UpdatedAt: newer.Add(time.Hour), Enabled: true, BotToken: "incomplete", ChatID: " "},
			},
			want: models.TelegramConfig{ID: 1, UpdatedAt: newer, Enabled: false, BotToken: "chosen", ChatID: "chosen-chat"},
		},
		{
			name: "retain_complete_singleton",
			configs: []models.TelegramConfig{
				{ID: 1, UpdatedAt: newer, Enabled: true, BotToken: "chosen", ChatID: "chosen-chat"},
				{ID: 2, UpdatedAt: older, Enabled: true, BotToken: "older", ChatID: "older-chat"},
			},
			want: models.TelegramConfig{ID: 1, UpdatedAt: newer, Enabled: true, BotToken: "chosen", ChatID: "chosen-chat"},
		},
		{
			name: "fall_back_to_latest_incomplete",
			configs: []models.TelegramConfig{
				{ID: 1, UpdatedAt: older, BotToken: "old"},
				{ID: 3, UpdatedAt: newer, ChatID: "chosen-chat"},
			},
			want: models.TelegramConfig{ID: 1, UpdatedAt: newer, ChatID: "chosen-chat"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := securityMigrationDB(t)
			if len(tc.configs) != 0 {
				// SQL preserves disabled values despite GORM's default tags.
				for _, cfg := range tc.configs {
					if err := db.Exec("INSERT INTO telegram_configs(id, updated_at, enabled, bot_token, chat_id) VALUES (?, ?, ?, ?, ?)", cfg.ID, cfg.UpdatedAt, cfg.Enabled, cfg.BotToken, cfg.ChatID).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			for pass := range 2 {
				if err := MigrateAndOptimize(db); err != nil {
					t.Fatal(err)
				}
				var got []models.TelegramConfig
				if err := db.Find(&got).Error; err != nil {
					t.Fatal(err)
				}
				if len(tc.configs) == 0 {
					if len(got) != 0 {
						t.Fatalf("unexpected default Telegram configuration: %+v", got)
					}
				} else if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
					t.Fatalf("pass %d: got %+v, want %+v", pass, got, tc.want)
				}
				assertPersonalSchema(t, db)
			}
		})
	}
}

func assertPersonalSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db.Migrator().HasTable("users") {
		t.Fatal("legacy users table remains")
	}
	for _, table := range []string{"email_accounts", "telegram_configs"} {
		if db.Migrator().HasColumn(table, "user_id") {
			t.Fatalf("%s.user_id remains", table)
		}
		var ddl []struct {
			SQL string
		}
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE tbl_name = ? AND sql IS NOT NULL", table).Scan(&ddl).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range ddl {
			if strings.Contains(strings.ToLower(row.SQL), "user_id") || strings.Contains(strings.ToLower(row.SQL), "references `users`") {
				t.Fatalf("legacy ownership constraint or index remains: %s", row.SQL)
			}
		}
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil || foreignKeys != 1 {
		t.Fatalf("foreign key enforcement was not restored: %d, %v", foreignKeys, err)
	}
}

// This fixture uses the legacy SQLite DDL, including GORM's inline foreign key
// constraint. Dropping just user_id with GORM's SQLite migrator is insufficient.
func legacyPersonalDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db := openSecurityMigrationDB(t)
	statements := []string{
		"CREATE TABLE users (id integer PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, deleted_at datetime, username text NOT NULL UNIQUE, password text NOT NULL, session_version integer NOT NULL DEFAULT 1, email text)",
		`CREATE TABLE email_accounts (
			id integer PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, deleted_at datetime,
			user_id integer NOT NULL, email text NOT NULL, provider text NOT NULL,
			imap_host text NOT NULL, imap_port integer NOT NULL, smtp_host text NOT NULL, smtp_port integer NOT NULL,
			username text NOT NULL, password text NOT NULL, last_sync_time datetime,
			uid_validity integer NOT NULL DEFAULT 0, status text DEFAULT 'active', color text DEFAULT '#6366f1',
			CONSTRAINT fk_users_accounts FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		"CREATE INDEX idx_email_accounts_user_id ON email_accounts(user_id)",
		"CREATE INDEX idx_email_accounts_deleted_at ON email_accounts(deleted_at)",
		`CREATE TABLE emails (
			id integer PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, deleted_at datetime,
			account_id integer NOT NULL, message_id text NOT NULL, "from" text NOT NULL, from_name text,
			"to" text NOT NULL, subject text, date datetime, body_text text, body_html text,
			body_fetched numeric NOT NULL DEFAULT false, has_attachment numeric DEFAULT false,
			is_read numeric DEFAULT false, pending_read numeric, is_starred numeric DEFAULT false,
			folder text DEFAULT 'INBOX', uid integer NOT NULL, uid_validity integer NOT NULL DEFAULT 0,
			CONSTRAINT fk_email_accounts_emails FOREIGN KEY (account_id) REFERENCES email_accounts(id) ON DELETE CASCADE
		)`,
		"CREATE TABLE telegram_configs (id integer PRIMARY KEY AUTOINCREMENT, user_id integer, updated_at datetime, enabled numeric DEFAULT false, bot_token text, chat_id text)",
		"CREATE UNIQUE INDEX idx_telegram_user ON telegram_configs(user_id) WHERE user_id > 0",
		"CREATE TABLE migration_email_changes (email_id integer)",
		"CREATE TRIGGER preserve_email_trigger AFTER UPDATE OF body_text ON emails BEGIN INSERT INTO migration_email_changes(email_id) VALUES (NEW.id); END",
		"INSERT INTO users(id, username, password) VALUES (1, 'first', 'hash'), (2, 'second', 'hash')",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestLegacyPersonalSchemaPreservesAccountsEmailsAndTriggers(t *testing.T) {
	db := legacyPersonalDatabase(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := credentials.Encrypt("remote-mail-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []struct {
		id        uint
		owner     uint
		deletedAt any
	}{{11, 1, nil}, {12, 2, "2026-01-02 03:04:05"}} {
		if err := db.Exec(`INSERT INTO email_accounts
			(id, user_id, created_at, updated_at, deleted_at, email, provider, imap_host, imap_port, smtp_host, smtp_port, username, password, last_sync_time, uid_validity, status, color)
			VALUES (?, ?, '2026-01-01 00:00:00', '2026-01-02 00:00:00', ?, 'mail@example.com', 'custom', 'imap.example.com', 993, 'smtp.example.com', 465, 'remote-user', ?, '2026-01-02 00:00:00', 77, 'active', '#123456')`, account.id, account.owner, account.deletedAt, sealed).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO emails
			(id, account_id, created_at, updated_at, deleted_at, message_id, "from", from_name, "to", subject, date, body_text, body_html, body_fetched, has_attachment, is_read, pending_read, is_starred, folder, uid, uid_validity)
			VALUES (?, ?, '2026-01-01 00:00:00', '2026-01-02 00:00:00', ?, 'message', 'sender@example.com', 'Sender', 'mail@example.com', 'subject', '2026-01-01 00:00:00', 'cached text', '<p>cached html</p>', 1, 1, 0, 1, 1, 'Archive', 99, 77)`, account.id+20, account.id, account.deletedAt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE sqlite_sequence SET seq = 100 WHERE name = 'email_accounts'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO telegram_configs(id, user_id, updated_at, enabled, bot_token, chat_id) VALUES
		(1, 0, '2026-01-01 00:00:00', 1, 'global-token', 'global-chat'),
		(4, 1, '2026-01-02 00:00:00', 1, 'chosen-token', 'chosen-chat'),
		(5, 2, '2026-01-03 00:00:00', 0, 'incomplete', '')`).Error; err != nil {
		t.Fatal(err)
	}
	var accountsBefore []models.EmailAccount
	var emailsBefore []models.Email
	if err := db.Unscoped().Order("id").Find(&accountsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Order("id").Find(&emailsBefore).Error; err != nil {
		t.Fatal(err)
	}
	var emailDDL string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE name = 'emails'").Scan(&emailDDL).Error; err != nil {
		t.Fatal(err)
	}
	for pass := range 2 {
		if err := MigrateAndOptimize(db); err != nil {
			t.Fatal(err)
		}
		assertPersonalSchema(t, db)
		var accountsAfter []models.EmailAccount
		var emailsAfter []models.Email
		if err := db.Unscoped().Order("id").Find(&accountsAfter).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Unscoped().Order("id").Find(&emailsAfter).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(accountsBefore, accountsAfter) || !reflect.DeepEqual(emailsBefore, emailsAfter) {
			t.Fatalf("pass %d changed account/email data: accounts %+v, emails %+v", pass, accountsAfter, emailsAfter)
		}
		var afterDDL string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE name = 'emails'").Scan(&afterDDL).Error; err != nil {
			t.Fatal(err)
		}
		if afterDDL != emailDDL {
			t.Fatal("emails table was rebuilt during personal migration")
		}
		var config models.TelegramConfig
		if err := db.First(&config).Error; err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Model(&models.TelegramConfig{}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 || config.ID != 1 || config.BotToken != "chosen-token" || config.ChatID != "chosen-chat" || !config.Enabled {
			t.Fatalf("legacy Telegram merge failed: %+v (%d rows)", config, count)
		}
		if err := migrateCredentials(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE emails SET body_text = 'updated cache' WHERE id = 31").Error; err != nil {
		t.Fatal(err)
	}
	var changes int64
	if err := db.Table("migration_email_changes").Count(&changes).Error; err != nil || changes != 1 {
		t.Fatalf("email trigger was lost: %d changes, %v", changes, err)
	}
	var sequence int64
	if err := db.Raw("SELECT seq FROM sqlite_sequence WHERE name = 'email_accounts'").Scan(&sequence).Error; err != nil || sequence != 100 {
		t.Fatalf("account ID sequence changed: %d, %v", sequence, err)
	}
}

func TestLegacyEmailColumnsAreAddedWithoutReplacingTriggers(t *testing.T) {
	db := legacyPersonalDatabase(t)
	for _, name := range []string{"body_fetched", "pending_read", "uid_validity"} {
		if err := db.Exec("ALTER TABLE emails DROP COLUMN " + name).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO email_accounts(id, user_id, email, provider, imap_host, imap_port, smtp_host, smtp_port, username, password)
		VALUES (7, 1, 'mail@example.com', 'custom', 'imap.example.com', 993, 'smtp.example.com', 465, 'remote-user', 'remote-password')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO emails(id, account_id, message_id, "from", "to", uid, body_text, body_html)
		VALUES (8, 7, 'old-message', 'sender@example.com', 'mail@example.com', 99, 'cached text', '<p>cached html</p>')`).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := MigrateAndOptimize(db); err != nil {
			t.Fatal(err)
		}
		var email models.Email
		if err := db.First(&email, 8).Error; err != nil {
			t.Fatal(err)
		}
		if email.AccountID != 7 || email.BodyText != "cached text" || email.BodyHTML != "<p>cached html</p>" || email.BodyFetched || email.PendingRead != nil || email.UIDValidity != 0 {
			t.Fatalf("additive email upgrade changed cached data: %+v", email)
		}
		for _, name := range []string{"body_fetched", "pending_read", "uid_validity"} {
			if !db.Migrator().HasColumn(&models.Email{}, name) {
				t.Fatalf("missing email column %s", name)
			}
		}
	}
	if err := db.Exec("UPDATE emails SET body_text = 'updated cache' WHERE id = 8").Error; err != nil {
		t.Fatal(err)
	}
	var changes int64
	if err := db.Table("migration_email_changes").Count(&changes).Error; err != nil || changes != 1 {
		t.Fatalf("additive migration lost the email trigger: %d changes, %v", changes, err)
	}
}

func TestPersonalSchemaMigrationRollsBackOnInvalidEmailReference(t *testing.T) {
	db := legacyPersonalDatabase(t)
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO emails(account_id, message_id, "from", "to", uid) VALUES (999, 'orphan', 'sender', 'recipient', 1)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateAndOptimize(db); err == nil {
		t.Fatal("invalid email reference must abort the migration")
	}
	if !db.Migrator().HasTable("users") || !db.Migrator().HasColumn("email_accounts", "user_id") || !db.Migrator().HasColumn("telegram_configs", "user_id") {
		t.Fatal("failed migration did not roll back the legacy schema")
	}
	for _, table := range []string{"email_accounts_personal", "telegram_configs_personal"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("failed migration left temporary table %s", table)
		}
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil || foreignKeys != 1 {
		t.Fatalf("foreign key enforcement was not restored after rollback: %d, %v", foreignKeys, err)
	}
}
