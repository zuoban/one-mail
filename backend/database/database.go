package database

import (
	"fmt"
	"log"
	"math/rand"
	"one-mail/backend/config"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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

	if err := seedAccountColors(); err != nil {
		return fmt.Errorf("failed to seed account colors: %w", err)
	}

	log.Println("Database connected and migrated successfully!")
	return nil
}

func seedAccountColors() error {
	var accounts []models.EmailAccount
	if err := DB.Where("color IS NULL OR color = ''").Find(&accounts).Error; err != nil {
		return err
	}

	for i := range accounts {
		accounts[i].Color = accountColors[rand.Intn(len(accountColors))]
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
