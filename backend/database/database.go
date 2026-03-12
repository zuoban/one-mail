package database

import (
	"fmt"
	"log"
	"one-mail/backend/config"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDatabase() error {
	var err error
	dsn := config.GetDSN()

	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := DB.AutoMigrate(
		&models.User{},
		&models.EmailAccount{},
		&models.Email{},
	); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database connected and migrated successfully!")
	return nil
}

func GetDB() *gorm.DB {
	return DB
}
