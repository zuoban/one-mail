package database

import (
	"log"
	"math/rand/v2"

	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

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

func seedAccountColors(db *gorm.DB) error {
	var accounts []models.EmailAccount
	if err := db.Where("color IS NULL OR color = ''").Find(&accounts).Error; err != nil {
		return err
	}

	for i := range accounts {
		accounts[i].Color = accountColors[rand.IntN(len(accountColors))]
	}

	if len(accounts) > 0 {
		if err := db.Save(&accounts).Error; err != nil {
			return err
		}
		log.Printf("Seeded colors for %d existing accounts", len(accounts))
	}

	return nil
}
