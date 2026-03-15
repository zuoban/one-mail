package handlers

import (
	"math/rand"
	"net/http"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"

	"github.com/gin-gonic/gin"
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

type AccountHandler struct{}

func NewAccountHandler() *AccountHandler {
	return &AccountHandler{}
}

func (h *AccountHandler) ListAccounts(c *gin.Context) {
	userID := c.GetUint("user_id")

	var accounts []models.EmailAccount
	if err := database.GetDB().Where("user_id = ?", userID).Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

type AddAccountRequest struct {
	Email          string `json:"email" binding:"required"`
	Provider       string `json:"provider" binding:"required"`
	Username       string `json:"username" binding:"required"`
	Password       string `json:"password" binding:"required"`
	IMAPHost       string `json:"imap_host"`
	IMAPPort       int    `json:"imap_port"`
	SMTPHost       string `json:"smtp_host"`
	SMTPPort       int    `json:"smtp_port"`
	Color          string `json:"color"`
	EnableAutoSync bool   `json:"enable_auto_sync"`
}

func (h *AccountHandler) AddAccount(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req AddAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	providerConfig, ok := imap.ProviderConfigs[req.Provider]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
		return
	}

	imapHost := req.IMAPHost
	imapPort := req.IMAPPort
	smtpHost := req.SMTPHost
	smtpPort := req.SMTPPort

	if imapHost == "" {
		imapHost = providerConfig.IMAPHost
		imapPort = providerConfig.IMAPPort
		smtpHost = providerConfig.SMTPHost
		smtpPort = providerConfig.SMTPPort
	}

	account := models.EmailAccount{
		UserID:         userID,
		Email:          req.Email,
		Provider:       req.Provider,
		IMAPHost:       imapHost,
		IMAPPort:       imapPort,
		SMTPHost:       smtpHost,
		SMTPPort:       smtpPort,
		Username:       req.Username,
		Password:       req.Password,
		Status:         "active",
		Color:          req.Color,
		EnableAutoSync: req.EnableAutoSync,
	}

	if account.Color == "" {
		account.Color = accountColors[rand.Intn(len(accountColors))]
	}

	testClient := imap.NewClient(&account)
	if err := testClient.TestConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to connect: " + err.Error()})
		return
	}

	if err := database.GetDB().Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	id := c.Param("id")

	if err := database.GetDB().Delete(&models.EmailAccount{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

type UpdateAccountRequest struct {
	Email          string `json:"email"`
	Provider       string `json:"provider"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Color          string `json:"color"`
	EnableAutoSync *bool  `json:"enable_auto_sync"`
}

func (h *AccountHandler) UpdateAccount(c *gin.Context) {
	userID := c.GetUint("user_id")
	id := c.Param("id")

	var account models.EmailAccount
	if err := database.GetDB().Where("user_id = ?", userID).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新字段（如果提供）
	if req.Email != "" {
		account.Email = req.Email
	}
	if req.Provider != "" {
		account.Provider = req.Provider
	}
	if req.Username != "" {
		account.Username = req.Username
	}
	if req.Password != "" {
		account.Password = req.Password
	}
	if req.Color != "" {
		account.Color = req.Color
	}
	if req.EnableAutoSync != nil {
		account.EnableAutoSync = *req.EnableAutoSync
	}

	if err := database.GetDB().Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) TestAccount(c *gin.Context) {
	id := c.Param("id")

	var account models.EmailAccount
	if err := database.GetDB().First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	testClient := imap.NewClient(&account)
	if err := testClient.TestConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "connection successful"})
}

func (h *AccountHandler) ListFolders(c *gin.Context) {
	userID := c.GetUint("user_id")
	id := c.Param("id")

	var account models.EmailAccount
	if err := database.GetDB().Where("user_id = ?", userID).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	client := imap.NewClient(&account)
	if err := client.Connect(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer client.Disconnect()

	folders, err := client.ListFoldersWithStatus()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": folders})
}
