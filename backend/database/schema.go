package database

import (
	"fmt"

	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

// InitializeSchema creates the current tables and indexes on a new database.
// Existing tables are left intact; startup still authenticates stored credentials
// and checks the optional full-text search index.
func InitializeSchema(db *gorm.DB) error {
	fts5Enabled.Store(false)
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []interface{}{
			&models.EmailAccount{}, &models.Email{}, &models.TelegramConfig{}, &models.NotificationTask{}, &models.NotificationDeliveryState{},
		} {
			if !tx.Migrator().HasTable(model) {
				if err := tx.Migrator().CreateTable(model); err != nil {
					return err
				}
			}
		}
		return validateCredentials(tx)
	}); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}
	setupFTS5(db)
	return nil
}
