package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"one-mail/backend/database"
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

func getOrCreateTelegramConfig() *models.TelegramConfig {
	db := database.GetDB()
	var cfg models.TelegramConfig
	if err := db.First(&cfg).Error; err != nil {
		cfg = models.TelegramConfig{
			Enabled:  false,
			BotToken: "",
			ChatID:   "",
		}
		db.Create(&cfg)
	}
	return &cfg
}

func GetTelegramConfig(c *gin.Context) {
	cfg := getOrCreateTelegramConfig()

	c.JSON(http.StatusOK, gin.H{
		"data": TelegramConfigResponse{
			Enabled:  cfg.Enabled,
			BotToken: maskToken(cfg.BotToken),
			ChatID:   cfg.ChatID,
		},
	})
}

func UpdateTelegramConfig(c *gin.Context) {
	var req TelegramConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	db := database.GetDB()
	cfg := getOrCreateTelegramConfig()

	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}

	if req.BotToken != "" && !strings.Contains(req.BotToken, "****") {
		cfg.BotToken = req.BotToken
	}

	if req.ChatID != "" {
		cfg.ChatID = req.ChatID
	}

	if err := db.Save(cfg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
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

func TestTelegramConnection(c *gin.Context) {
	var req TelegramConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg := getOrCreateTelegramConfig()

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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "测试消息发送成功"})
}

func SendEmailToTelegram(c *gin.Context) {
	emailID := c.Param("id")
	if emailID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email id is required"})
		return
	}

	cfg := getOrCreateTelegramConfig()

	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram notification is not configured"})
		return
	}

	db := database.GetDB()

	var email models.Email
	if err := db.First(&email, emailID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	var account models.EmailAccount
	if err := db.First(&account, email.AccountID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	client := telegram.GetClient()
	if err := client.SendNewEmailWithConfig(&email, account.Email, cfg.BotToken, cfg.ChatID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
