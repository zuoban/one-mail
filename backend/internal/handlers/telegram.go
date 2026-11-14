package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"one-mail/backend/database"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"
)

type TelegramConfigResponse struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type TelegramConfigRequest struct {
	Enabled  *bool  `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

func getOrCreateTelegramConfig() (*models.TelegramConfig, error) {
	db := database.GetDB()
	if db == nil {
		return nil, errors.New("telegram configuration unavailable")
	}
	var cfg models.TelegramConfig
	err := db.Where("id = ?", 1).First(&cfg).Error
	if err == nil {
		return &cfg, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	cfg = models.TelegramConfig{ID: 1}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&cfg).Error; err != nil {
		return nil, err
	}
	var saved models.TelegramConfig
	if err := db.Where("id = ?", 1).First(&saved).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func telegramConfigForRequest(c *gin.Context) (*models.TelegramConfig, bool) {
	cfg, err := getOrCreateTelegramConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load telegram configuration"})
		return nil, false
	}
	cfg.BotToken, err = credentials.Decrypt(cfg.BotToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unlock telegram configuration"})
		return nil, false
	}
	return cfg, true
}

func GetTelegramConfig(c *gin.Context) {
	cfg, ok := telegramConfigForRequest(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": TelegramConfigResponse{
			Enabled:  cfg.Enabled,
			BotToken: maskToken(cfg.BotToken),
			ChatID:   cfg.ChatID,
		},
	})
}

func UpdateTelegramConfig(c *gin.Context) {
	cfg, ok := telegramConfigForRequest(c)
	if !ok {
		return
	}
	var req TelegramConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram configuration request"})
		return
	}

	db := database.GetDB()

	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}

	updates := map[string]interface{}{"enabled": cfg.Enabled}
	if req.BotToken != "" && !strings.Contains(req.BotToken, "****") {
		sealed, err := credentials.Encrypt(req.BotToken)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to secure telegram configuration"})
			return
		}
		cfg.BotToken = req.BotToken
		updates["bot_token"] = sealed
	}

	if req.ChatID != "" {
		cfg.ChatID = req.ChatID
	}
	updates["chat_id"] = cfg.ChatID

	if err := db.Model(&models.TelegramConfig{}).Where("id = ?", 1).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}
	telegram.InvalidateConfigCache()

	c.JSON(http.StatusOK, gin.H{
		"data": TelegramConfigResponse{
			Enabled:  cfg.Enabled,
			BotToken: maskToken(cfg.BotToken),
			ChatID:   cfg.ChatID,
		},
	})
}

func TestTelegramConnection(c *gin.Context) {
	cfg, ok := telegramConfigForRequest(c)
	if !ok {
		return
	}
	var req TelegramConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram configuration request"})
		return
	}

	botToken := req.BotToken
	if botToken == "" || strings.Contains(botToken, "****") {
		botToken = cfg.BotToken
	}

	chatID := req.ChatID
	if chatID == "" {
		chatID = cfg.ChatID
	}

	if botToken == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_token and chat_id are required"})
		return
	}

	client := telegram.GetClient()
	if err := client.TestConnection(botToken, chatID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram connection test failed; check bot token and chat ID"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "测试消息发送成功"})
}

func SendEmailToTelegram(c *gin.Context) {
	cfg, ok := telegramConfigForRequest(c)
	if !ok {
		return
	}
	emailID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || emailID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid email id is required"})
		return
	}

	db := database.GetDB()
	var email models.Email
	err = db.Joins("JOIN email_accounts ON email_accounts.id = emails.account_id AND email_accounts.deleted_at IS NULL").
		Where("emails.id = ?", emailID).First(&email).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load email"})
		return
	}
	var account models.EmailAccount
	if err := db.Where("id = ?", email.AccountID).First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}
	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram notification is not configured"})
		return
	}
	if err := telegram.GetClient().SendNewEmailWithConfig(&email, account.Email, cfg.BotToken, cfg.ChatID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send telegram notification"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "邮件已发送到 Telegram"})
}

func maskToken(token string) string {
	if len(token) > 8 {
		return token[:4] + "****" + token[len(token)-4:]
	} else if len(token) > 0 {
		return "****"
	}
	return ""
}
