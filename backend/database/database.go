package database

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"one-mail/backend/config"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var fts5Enabled atomic.Bool

func IsFTS5Enabled() bool {
	return fts5Enabled.Load()
}

var DB *gorm.DB

var accountColors = []string{
	"#6366f1", // indigo
	"#8b5cf6", // violet
	"#ec4899", // pink
	"#f43f5e", // rose
	"#f97316", // orange
	"#eab308", // yellow
	"#22c55e", // green
	"#14b8a6", // teal
	"#06b6d4", // cyan
	"#3b82f6", // blue
}

func InitDatabase() error {
	var err error
	dsn := config.GetDSN()
	if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	if err := credentials.Init(os.Getenv("CREDENTIALS_KEY"), dsn+".key"); err != nil {
		return fmt.Errorf("initialize credential encryption: %w", err)
	}

	logLevel := logger.Warn
	if config.AppConfig != nil && config.AppConfig.Server.Mode == "release" {
		logLevel = logger.Error
	}
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

	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := SetupSQLite(DB, config.AppConfig != nil && config.AppConfig.Database.Type == "sqlite"); err != nil {
		return err
	}

	if err := MigrateAndOptimize(DB); err != nil {
		return err
	}
	if err := migrateCredentials(DB); err != nil {
		return err
	}

	if err := seedAccountColors(); err != nil {
		return fmt.Errorf("failed to seed account colors: %w", err)
	}

	log.Println("Database connected and migrated successfully!")
	return nil
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

// MigrateAndOptimize removes legacy application users, migrates the personal
// instance schema, fixes composite indexes, and sets up FTS5 if compiled in.
func MigrateAndOptimize(db *gorm.DB) error {
	// SQLite cannot disable foreign keys inside a transaction. Pin the connection
	// so the table cutover and restoration use the same connection-scoped setting.
	if err := db.Connection(func(conn *gorm.DB) (result error) {
		// Connection hands out a handle that reuses its Statement. Start a clean
		// session so PRAGMA scans cannot leak their destination into later queries.
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
		return conn.Transaction(func(tx *gorm.DB) error {
			// SQLite table replacements reset sqlite_sequence to MAX(id). Save the
			// account high-water mark before either our cutover or GORM can rebuild.
			var accountSequence int64
			if tx.Migrator().HasTable("sqlite_sequence") {
				if err := tx.Raw("SELECT COALESCE(MAX(seq), 0) FROM sqlite_sequence WHERE name = 'email_accounts'").Scan(&accountSequence).Error; err != nil {
					return err
				}
			}
			if err := migratePersonalSchema(tx); err != nil {
				return err
			}
			if err := migratePersonalModels(tx); err != nil {
				return err
			}
			if err := mergeTelegramConfig(tx); err != nil {
				return err
			}
			if err := ensureEmailIndexes(tx); err != nil {
				return err
			}
			// Restore only after every schema migration, including AutoMigrate.
			// Repeating startup must never lower a previously reserved account ID.
			if accountSequence > 0 {
				if err := tx.Exec("INSERT INTO sqlite_sequence(name, seq) SELECT 'email_accounts', ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'email_accounts')", accountSequence).Error; err != nil {
					return err
				}
				if err := tx.Exec("UPDATE sqlite_sequence SET seq = MAX(COALESCE(seq, 0), ?) WHERE name = 'email_accounts'", accountSequence).Error; err != nil {
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
			return nil
		})
	}); err != nil {
		return fmt.Errorf("failed to migrate personal database: %w", err)
	}

	setupFTS5(db)
	return nil
}

func migratePersonalModels(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.Email{}) {
		return tx.AutoMigrate(&models.EmailAccount{}, &models.Email{}, &models.TelegramConfig{})
	}

	// AlterColumn on SQLite replaces the table and drops its triggers. Existing
	// emails need only additive migrations, including installations with cached
	// bodies predating body_fetched, pending_read, and uid_validity.
	independent := tx.Session(&gorm.Session{})
	independent.Config.IgnoreRelationshipsWhenMigrating = true
	if err := independent.AutoMigrate(&models.EmailAccount{}, &models.TelegramConfig{}); err != nil {
		return err
	}
	stmt := &gorm.Statement{DB: tx}
	if err := stmt.Parse(&models.Email{}); err != nil {
		return err
	}
	migrator := tx.Migrator()
	for _, name := range stmt.Schema.DBNames {
		if !migrator.HasColumn(&models.Email{}, name) {
			if err := migrator.AddColumn(&models.Email{}, name); err != nil {
				return err
			}
		}
	}
	for _, index := range stmt.Schema.ParseIndexes() {
		if !migrator.HasIndex(&models.Email{}, index.Name) {
			if err := migrator.CreateIndex(&models.Email{}, index.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateCredentials(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var accounts []models.EmailAccount
		if err := tx.Unscoped().Select("id, password").Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if credentials.IsEncrypted(account.Password) {
				if _, err := credentials.Decrypt(account.Password); err != nil {
					return fmt.Errorf("cannot unlock credentials for account %d: %w", account.ID, err)
				}
				continue
			}
			sealed, err := credentials.Encrypt(account.Password)
			if err != nil {
				return err
			}
			if err := tx.Unscoped().Model(&models.EmailAccount{}).Where("id = ?", account.ID).
				UpdateColumn("password", sealed).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Only the two tables containing user_id are rebuilt. The emails table and its
// existing account foreign key, body cache, pending reads, and FTS triggers stay
// in place. Copying SQL values also preserves encrypted passwords and soft deletes.
func migratePersonalSchema(tx *gorm.DB) error {
	if tx.Migrator().HasColumn("email_accounts", "user_id") {
		if err := rebuildPersonalTable(tx, "email_accounts", `CREATE TABLE email_accounts_personal (
			id integer PRIMARY KEY AUTOINCREMENT,
			created_at datetime, updated_at datetime, deleted_at datetime,
			email text NOT NULL, provider text NOT NULL,
			imap_host text NOT NULL, imap_port integer NOT NULL,
			smtp_host text NOT NULL, smtp_port integer NOT NULL,
			username text NOT NULL, password text NOT NULL,
			last_sync_time datetime, uid_validity integer NOT NULL DEFAULT 0,
			status text DEFAULT 'active', color text DEFAULT '#6366f1'
		)`); err != nil {
			return fmt.Errorf("remove email account ownership: %w", err)
		}
	}
	if tx.Migrator().HasColumn("telegram_configs", "user_id") {
		if err := rebuildPersonalTable(tx, "telegram_configs", `CREATE TABLE telegram_configs_personal (
			id integer PRIMARY KEY AUTOINCREMENT, updated_at datetime,
			enabled numeric DEFAULT false, bot_token text, chat_id text
		)`); err != nil {
			return fmt.Errorf("remove Telegram ownership: %w", err)
		}
	}
	return tx.Exec("DROP TABLE IF EXISTS users").Error
}

func rebuildPersonalTable(tx *gorm.DB, table, ddl string) error {
	// The SQLite driver's DDL parser treats tabs as quoting characters. Emit
	// compact indentation so later schema inspection sees every column.
	if err := tx.Exec(strings.ReplaceAll(ddl, "\t", "")).Error; err != nil {
		return err
	}
	// Older installations may not yet have newer columns such as uid_validity.
	// Copy only shared columns so the new columns receive their schema defaults.
	type column struct {
		Name string
	}
	var previous, current []column
	if err := tx.Raw("PRAGMA table_info(" + table + ")").Scan(&previous).Error; err != nil {
		return err
	}
	if err := tx.Raw("PRAGMA table_info(" + table + "_personal)").Scan(&current).Error; err != nil {
		return err
	}
	available := make(map[string]bool, len(previous))
	for _, col := range previous {
		available[col.Name] = true
	}
	columns := make([]string, 0, len(current))
	for _, col := range current {
		if available[col.Name] {
			columns = append(columns, `"`+col.Name+`"`)
		}
	}
	projection := strings.Join(columns, ", ")
	if err := tx.Exec("INSERT INTO " + table + "_personal (" + projection + ") SELECT " + projection + " FROM " + table).Error; err != nil {
		return err
	}
	if err := tx.Exec("DROP TABLE " + table).Error; err != nil {
		return err
	}
	return tx.Exec("ALTER TABLE " + table + "_personal RENAME TO " + table).Error
}

func mergeTelegramConfig(tx *gorm.DB) error {
	var cfg models.TelegramConfig
	err := tx.Order("CASE WHEN TRIM(COALESCE(bot_token, '')) <> '' AND TRIM(COALESCE(chat_id, '')) <> '' THEN 0 ELSE 1 END").
		Order("updated_at DESC").Order("id DESC").First(&cfg).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if cfg.ID == 1 {
		return tx.Where("id <> ?", 1).Delete(&models.TelegramConfig{}).Error
	}
	if err := tx.Where("1 = 1").Delete(&models.TelegramConfig{}).Error; err != nil {
		return err
	}
	cfg.ID = 1
	return tx.Create(&cfg).Error
}

func ensureEmailIndexes(db *gorm.DB) error {
	type indexCol struct {
		Seqno int
		Cid   int
		Name  string
	}

	var cols []indexCol
	if err := db.Raw("PRAGMA index_info('idx_account_folder_date')").Scan(&cols).Error; err == nil && len(cols) >= 2 {
		// Legacy index had (account_id, date, folder); rebuild if folder is not the second column.
		if cols[1].Name != "folder" {
			if err := db.Exec("DROP INDEX IF EXISTS idx_account_folder_date").Error; err != nil {
				return err
			}
		}
	}

	// Drop obsolete idx_account_read if present in older schemas.
	_ = db.Exec("DROP INDEX IF EXISTS idx_account_read").Error

	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_account_folder_date ON emails(account_id, folder, date DESC, id DESC) WHERE deleted_at IS NULL").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_account_folder_read_date ON emails(account_id, folder, is_read, date DESC, id DESC) WHERE deleted_at IS NULL").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_folder_date ON emails(folder, date DESC, id DESC, account_id) WHERE deleted_at IS NULL").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_folder_read_date ON emails(folder, is_read, date DESC, id DESC, account_id) WHERE deleted_at IS NULL").Error; err != nil {
		return err
	}
	return nil
}

func setupFTS5(db *gorm.DB) {
	var existingDDL string
	_ = db.Raw("SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name='emails_fts'").Scan(&existingDDL).Error
	if existingDDL != "" && (strings.Contains(existingDDL, "content='emails'") || strings.Contains(existingDDL, "sender")) {
		_ = db.Exec("DROP TRIGGER IF EXISTS emails_fts_ai").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS emails_fts_ad").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS emails_fts_au").Error
		_ = db.Exec("DROP TABLE IF EXISTS emails_fts").Error
	}

	ftsDDL := `CREATE VIRTUAL TABLE IF NOT EXISTS emails_fts USING fts5(
		subject,
		"from",
		from_name,
		tokenize='trigram'
	);`
	if err := db.Exec(ftsDDL).Error; err != nil {
		fts5Enabled.Store(false)
		log.Printf("SQLite FTS5 not enabled (%v); falling back to indexed LIKE search", err)
		return
	}

	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS emails_fts_ai AFTER INSERT ON emails WHEN NEW.deleted_at IS NULL BEGIN
			INSERT OR REPLACE INTO emails_fts(rowid, subject, "from", from_name)
			VALUES (NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, ''));
		END;`,
		`CREATE TRIGGER IF NOT EXISTS emails_fts_ad AFTER DELETE ON emails BEGIN
			DELETE FROM emails_fts WHERE rowid = OLD.id;
		END;`,
		`CREATE TRIGGER IF NOT EXISTS emails_fts_au AFTER UPDATE OF subject, "from", from_name, deleted_at ON emails BEGIN
			DELETE FROM emails_fts WHERE rowid = OLD.id;
			INSERT INTO emails_fts(rowid, subject, "from", from_name)
			SELECT NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, '')
			WHERE NEW.deleted_at IS NULL;
		END;`,
	}
	for _, trig := range triggers {
		if err := db.Exec(trig).Error; err != nil {
			fts5Enabled.Store(false)
			log.Printf("Failed to create FTS5 trigger (%v); disabling FTS5", err)
			return
		}
	}

	var needsBackfill bool
	if err := db.Raw("SELECT (NOT EXISTS(SELECT 1 FROM emails_fts)) AND EXISTS(SELECT 1 FROM emails WHERE deleted_at IS NULL)").Scan(&needsBackfill).Error; err == nil && needsBackfill {
		if err := db.Exec(`INSERT OR REPLACE INTO emails_fts(rowid, subject, "from", from_name)
			SELECT id, COALESCE(subject, ''), COALESCE("from", ''), COALESCE(from_name, '')
			FROM emails WHERE deleted_at IS NULL`).Error; err != nil {
			log.Printf("Warning: FTS5 backfill failed: %v", err)
		}
	}

	fts5Enabled.Store(true)
}

func seedAccountColors() error {
	var accounts []models.EmailAccount
	if err := DB.Where("color IS NULL OR color = ''").Find(&accounts).Error; err != nil {
		return err
	}

	for i := range accounts {
		accounts[i].Color = accountColors[rand.IntN(len(accountColors))]
	}

	if len(accounts) > 0 {
		if err := DB.Save(&accounts).Error; err != nil {
			return err
		}
		log.Printf("Seeded colors for %d existing accounts", len(accounts))
	}

	return nil
}

func GetDB() *gorm.DB {
	return DB
}
