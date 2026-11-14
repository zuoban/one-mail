package database

import (
	"fmt"
	"strings"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

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

// Only the two tables containing user_id are rebuilt. The email account foreign
// key and current body cache remain in place; the cache migration removes legacy
// mutation state afterward. Encrypted account passwords keep their SQL values.
// The retired color column remains in this legacy rebuild to preserve stored
// values; current account models neither read nor write it.
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
