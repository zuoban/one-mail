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

// MigrateAndOptimize runs schema AutoMigrate, fixes misaligned composite indexes, and sets up FTS5 if compiled in.
func MigrateAndOptimize(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.User{},
		&models.EmailAccount{},
		&models.Email{},
		&models.TelegramConfig{},
	); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}
	if err := migrateUserSettings(db); err != nil {
		return fmt.Errorf("migrate user settings: %w", err)
	}

	if err := ensureEmailIndexes(db); err != nil {
		return fmt.Errorf("failed to optimize email indexes: %w", err)
	}

	setupFTS5(db)
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

// A legacy global Telegram configuration can be assigned only when its owner
// is unambiguous. Other legacy rows remain disabled and inaccessible.
func migrateUserSettings(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("session_version IS NULL OR session_version = 0").
			UpdateColumn("session_version", 1).Error; err != nil {
			return err
		}
		var users []models.User
		if err := tx.Select("id").Limit(2).Find(&users).Error; err != nil {
			return err
		}
		legacy := "user_id IS NULL OR user_id = 0"
		if len(users) == 1 {
			var owned int64
			if err := tx.Model(&models.TelegramConfig{}).Where("user_id = ?", users[0].ID).Count(&owned).Error; err != nil {
				return err
			}
			if owned == 0 {
				var cfg models.TelegramConfig
				err := tx.Where(legacy).Order("id").First(&cfg).Error
				if err != nil && err != gorm.ErrRecordNotFound {
					return err
				}
				if err == nil {
					if err := tx.Model(&cfg).UpdateColumn("user_id", users[0].ID).Error; err != nil {
						return err
					}
				}
			}
		}
		return tx.Model(&models.TelegramConfig{}).Where(legacy).UpdateColumn("enabled", false).Error
	})
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
