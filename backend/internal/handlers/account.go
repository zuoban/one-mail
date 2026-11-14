package handlers

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"github.com/gin-gonic/gin"
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
	Email    string `json:"email" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Color    string `json:"color"`
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
		UserID:   userID,
		Email:    req.Email,
		Provider: req.Provider,
		IMAPHost: imapHost,
		IMAPPort: imapPort,
		SMTPHost: smtpHost,
		SMTPPort: smtpPort,
		Username: req.Username,
		Password: req.Password,
		Status:   "active",
		Color:    req.Color,
	}

	if account.Color == "" {
		account.Color = accountColors[rand.Intn(len(accountColors))]
	}

	testClient := imap.NewClient(&account)
	if err := testClient.TestConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to connect: " + err.Error()})
		return
	}
	sealed, err := credentials.Encrypt(account.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to protect account credentials"})
		return
	}
	account.Password = sealed

	if err := database.GetDB().Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if _, err := mail.RefreshAccount(ctx, database.GetDB(), &account, mail.MaxLatestEmails); err != nil {
		log.Printf("Initial fetch for account %d failed: %v", account.ID, err)
	}

	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	userID := c.GetUint("user_id")
	id := c.Param("id")

	var account models.EmailAccount
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND user_id = ?", id, userID).First(&account).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&models.Email{}).Error; err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
	if err == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete account"})
		return
	}
	imap.GetConnectionPool().RemoveConnection(account.ID)

	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

type UpdateAccountRequest struct {
	Email    string `json:"email"`
	Provider string `json:"provider"`
	Username string `json:"username"`
	Password string `json:"password"`
	Color    string `json:"color"`
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
	credentialsChanged := (req.Provider != "" && req.Provider != account.Provider) ||
		(req.Username != "" && req.Username != account.Username) || req.Password != ""

	// 更新字段（如果提供）
	if req.Email != "" {
		account.Email = req.Email
	}
	if req.Provider != "" && req.Provider != account.Provider {
		provider, ok := imap.ProviderConfigs[req.Provider]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
			return
		}
		account.Provider = req.Provider
		account.IMAPHost, account.IMAPPort = provider.IMAPHost, provider.IMAPPort
		account.SMTPHost, account.SMTPPort = provider.SMTPHost, provider.SMTPPort
	}
	if req.Username != "" {
		account.Username = req.Username
	}
	if req.Password != "" {
		sealed, err := credentials.Encrypt(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to protect account credentials"})
			return
		}
		account.Password = sealed
	}
	if req.Color != "" {
		account.Color = req.Color
	}

	if err := database.GetDB().Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if credentialsChanged {
		imap.GetConnectionPool().RemoveConnection(account.ID)
	}

	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) TestAccount(c *gin.Context) {
	userID := c.GetUint("user_id")
	id := c.Param("id")

	// 验证账户所有权
	var account models.EmailAccount
	if err := database.GetDB().Where("id = ? AND user_id = ?", id, userID).First(&account).Error; err != nil {
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
