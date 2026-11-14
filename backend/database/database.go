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
	fts5Enabled.Store(false)
	var err error
	dsn := config.GetDSN()
	if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	if err := credentials.Init(os.Getenv("CREDENTIALS_KEY"), dsn+".key"); err != nil {
		return fmt.Errorf("initialize credential encryption: %w", err)
	}

	// Keep slow-query warnings in release builds; parameters remain redacted.
	logLevel := logger.Warn
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
	fts5Enabled.Store(false)
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
			if err := migrateRecentMailCache(tx); err != nil {
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

	// Add missing fields and indexes without replacing existing tables, so
	// unrelated triggers and cached message bodies survive version upgrades.
	for _, model := range []interface{}{&models.EmailAccount{}, &models.Email{}, &models.TelegramConfig{}} {
		if !tx.Migrator().HasTable(model) {
			if err := tx.AutoMigrate(model); err != nil {
				return err
			}
			continue
		}
		stmt := &gorm.Statement{DB: tx}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		migrator := tx.Migrator()
		for _, name := range stmt.Schema.DBNames {
			if !migrator.HasColumn(model, name) {
				if err := migrator.AddColumn(model, name); err != nil {
					return err
				}
			}
		}
		for _, index := range stmt.Schema.ParseIndexes() {
			if !migrator.HasIndex(model, index.Name) {
				if err := migrator.CreateIndex(model, index.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// migrateRecentMailCache removes obsolete mutation state and bounds stored mail.
// Native DROP COLUMN preserves unrelated triggers and the email ID sequence.
func migrateRecentMailCache(tx *gorm.DB) error {
	obsoleteColumns := []string{"deleted_at", "is_read", "pending_read", "is_starred"}
	needsCutover := false
	for _, name := range obsoleteColumns {
		needsCutover = needsCutover || tx.Migrator().HasColumn("emails", name)
	}
	if needsCutover {
		for _, name := range []string{"emails_fts_ai", "emails_fts_ad", "emails_fts_au"} {
			if err := tx.Exec("DROP TRIGGER IF EXISTS " + name).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec("DROP TABLE IF EXISTS emails_fts").Error; err != nil {
			return err
		}
		if tx.Migrator().HasColumn("emails", "deleted_at") {
			if err := tx.Exec("DELETE FROM emails WHERE deleted_at IS NOT NULL").Error; err != nil {
				return err
			}
		}
		var indexes []struct {
			Name string
			SQL  string
		}
		if err := tx.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'emails' AND sql IS NOT NULL").Scan(&indexes).Error; err != nil {
			return err
		}
		for _, index := range indexes {
			if !indexReferencesColumns(index.SQL, obsoleteColumns) {
				continue
			}
			name := strings.ReplaceAll(index.Name, `"`, `""`)
			if err := tx.Exec(`DROP INDEX "` + name + `"`).Error; err != nil {
				return err
			}
		}
		for _, name := range obsoleteColumns {
			if tx.Migrator().HasColumn("emails", name) {
				if err := tx.Exec(`ALTER TABLE emails DROP COLUMN "` + name + `"`).Error; err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Exec(`DELETE FROM emails WHERE account_id IN
		(SELECT id FROM email_accounts WHERE deleted_at IS NOT NULL)
		OR folder IS NULL OR folder <> 'INBOX'`).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM emails WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY account_id ORDER BY date DESC, id DESC) AS cache_rank
			FROM emails
		) WHERE cache_rank > 50
	)`).Error
}

// Inspect identifiers in index expressions and predicates, without treating
// index names, SQL string literals, or comments as column dependencies.
func indexReferencesColumns(ddl string, columns []string) bool {
	matches := func(token string) bool {
		for _, column := range columns {
			if strings.EqualFold(token, column) {
				return true
			}
		}
		return false
	}
	inDefinition := false
	for offset := 0; offset < len(ddl); {
		if strings.HasPrefix(ddl[offset:], "--") {
			end := strings.IndexByte(ddl[offset:], '\n')
			if end < 0 {
				return false
			}
			offset += end + 1
			continue
		}
		if strings.HasPrefix(ddl[offset:], "/*") {
			end := strings.Index(ddl[offset+2:], "*/")
			if end < 0 {
				return false
			}
			offset += end + 4
			continue
		}
		switch quote := ddl[offset]; quote {
		case '\'', '"', '`', '[':
			endQuote := quote
			if quote == '[' {
				endQuote = ']'
			}
			offset++
			start := offset
			for offset < len(ddl) {
				if ddl[offset] == endQuote {
					if offset+1 < len(ddl) && ddl[offset+1] == endQuote {
						offset += 2
						continue
					}
					break
				}
				offset++
			}
			if inDefinition && quote != '\'' && matches(ddl[start:offset]) {
				return true
			}
			if offset < len(ddl) {
				offset++
			}
			continue
		case '(':
			inDefinition = true
		}
		if isSQLIdentifierByte(ddl[offset]) {
			start := offset
			for offset < len(ddl) && isSQLIdentifierByte(ddl[offset]) {
				offset++
			}
			if inDefinition && matches(ddl[start:offset]) {
				return true
			}
			continue
		}
		offset++
	}
	return false
}

func isSQLIdentifierByte(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' || char == '_' || char >= 0x80
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
		var configs []models.TelegramConfig
		if err := tx.Select("id, bot_token").Find(&configs).Error; err != nil {
			return err
		}
		for _, cfg := range configs {
			if cfg.BotToken == "" {
				continue
			}
			if credentials.IsEncrypted(cfg.BotToken) {
				if _, err := credentials.Decrypt(cfg.BotToken); err != nil {
					return fmt.Errorf("cannot unlock Telegram credentials for configuration %d: %w", cfg.ID, err)
				}
				continue
			}
			sealed, err := credentials.Encrypt(cfg.BotToken)
			if err != nil {
				return err
			}
			if err := tx.Model(&models.TelegramConfig{}).Where("id = ?", cfg.ID).
				UpdateColumn("bot_token", sealed).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Only the two tables containing user_id are rebuilt. The email account foreign
// key and current body cache remain in place; the cache migration removes legacy
// mutation state afterward. Encrypted account passwords keep their SQL values.
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
			identity_version integer NOT NULL DEFAULT 0, config_version integer NOT NULL DEFAULT 0,
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
	var triggers []struct{ SQL string }
	if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type = 'trigger' AND tbl_name = ? AND sql IS NOT NULL", table).Scan(&triggers).Error; err != nil {
		return err
	}
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
	if err := tx.Exec("ALTER TABLE " + table + "_personal RENAME TO " + table).Error; err != nil {
		return err
	}
	for _, trigger := range triggers {
		if err := tx.Exec(trigger.SQL).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergeTelegramConfig(tx *gorm.DB) error {
	// Authenticate every ciphertext before singleton selection discards rows.
	var configs []models.TelegramConfig
	if err := tx.Select("id, bot_token").Find(&configs).Error; err != nil {
		return err
	}
	for _, cfg := range configs {
		if credentials.IsEncrypted(cfg.BotToken) {
			if _, err := credentials.Decrypt(cfg.BotToken); err != nil {
				return fmt.Errorf("cannot unlock Telegram credentials for configuration %d: %w", cfg.ID, err)
			}
		}
	}
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

	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_account_folder_date ON emails(account_id, folder, date DESC, id DESC)").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_folder_date ON emails(folder, date DESC, id DESC, account_id)").Error; err != nil {
		return err
	}
	return nil
}

func setupFTS5(db *gorm.DB) {
	fts5Enabled.Store(false)
	err := db.Transaction(func(tx *gorm.DB) error {
		var existingDDL string
		if err := tx.Raw("SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name='emails_fts'").Scan(&existingDDL).Error; err != nil {
			return err
		}
		// Replace old trigger definitions as part of the same atomic setup.
		for _, name := range []string{"emails_fts_ai", "emails_fts_ad", "emails_fts_au"} {
			if err := tx.Exec("DROP TRIGGER IF EXISTS " + name).Error; err != nil {
				return err
			}
		}
		if existingDDL != "" && (strings.Contains(existingDDL, "content='emails'") || strings.Contains(existingDDL, "sender")) {
			if err := tx.Exec("DROP TABLE emails_fts").Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS emails_fts USING fts5(
			subject, "from", from_name, tokenize='trigram'
		)`).Error; err != nil {
			return err
		}
		triggers := []string{
			`CREATE TRIGGER emails_fts_ai AFTER INSERT ON emails BEGIN
				INSERT INTO emails_fts(rowid, subject, "from", from_name)
				VALUES (NEW.id, COALESCE(NEW.subject, ''), COALESCE(NEW."from", ''), COALESCE(NEW.from_name, ''));
			END;`,
			`CREATE TRIGGER emails_fts_ad AFTER DELETE ON emails BEGIN
				DELETE FROM emails_fts WHERE rowid = OLD.id;
			END;`,
			`CREATE TRIGGER emails_fts_au AFTER UPDATE OF subject, "from", from_name ON emails
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
