package database

import (
	"fmt"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

func validateCredentials(db *gorm.DB) error {
	var accounts []models.EmailAccount
	if err := db.Unscoped().Select("id, password").Find(&accounts).Error; err != nil {
		return err
	}
	for _, account := range accounts {
		if !credentials.IsEncrypted(account.Password) {
			return fmt.Errorf("credentials for account %d are not encrypted", account.ID)
		}
		if _, err := credentials.Decrypt(account.Password); err != nil {
			return fmt.Errorf("cannot unlock credentials for account %d: %w", account.ID, err)
		}
	}
	var configs []models.TelegramConfig
	if err := db.Select("id, bot_token").Find(&configs).Error; err != nil {
		return err
	}
	for _, cfg := range configs {
		if cfg.BotToken == "" {
			continue
		}
		if !credentials.IsEncrypted(cfg.BotToken) {
			return fmt.Errorf("Telegram credentials for configuration %d are not encrypted", cfg.ID)
		}
		if _, err := credentials.Decrypt(cfg.BotToken); err != nil {
			return fmt.Errorf("cannot unlock Telegram credentials for configuration %d: %w", cfg.ID, err)
		}
	}
	return nil
}
