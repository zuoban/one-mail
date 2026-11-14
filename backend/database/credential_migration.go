package database

import (
	"fmt"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

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

// Ciphertext authentication remains a startup check after the plaintext
// conversion has been recorded. Include soft-deleted accounts and all Telegram
// rows before any historical singleton migration can discard credentials.
func validateCredentials(db *gorm.DB) error {
	if db.Migrator().HasTable(&models.EmailAccount{}) {
		var accounts []models.EmailAccount
		if err := db.Unscoped().Select("id, password").Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if credentials.IsEncrypted(account.Password) {
				if _, err := credentials.Decrypt(account.Password); err != nil {
					return fmt.Errorf("cannot unlock credentials for account %d: %w", account.ID, err)
				}
			}
		}
	}
	if db.Migrator().HasTable(&models.TelegramConfig{}) {
		var configs []models.TelegramConfig
		if err := db.Select("id, bot_token").Find(&configs).Error; err != nil {
			return err
		}
		for _, cfg := range configs {
			if credentials.IsEncrypted(cfg.BotToken) {
				if _, err := credentials.Decrypt(cfg.BotToken); err != nil {
					return fmt.Errorf("cannot unlock Telegram credentials for configuration %d: %w", cfg.ID, err)
				}
			}
		}
	}
	return nil
}
