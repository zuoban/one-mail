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
	var accounts []models.EmailAccount
	if err := database.GetDB().Find(&accounts).Error; err != nil {
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
	if err := testClient.TestConnectionContext(c.Request.Context()); err != nil {
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
	id := c.Param("id")

	var account models.EmailAccount
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).First(&account).Error; err != nil {
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
	imap.GetConnectionPool().RemoveConnection(account.ID, account.ConfigVersion+1)

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
	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Provider != "" {
		if _, ok := imap.ProviderConfigs[req.Provider]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
			return
		}
	}
	var sealed string
	if req.Password != "" {
		var err error
		sealed, err = credentials.Encrypt(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to protect account credentials"})
			return
		}
	}
	var account models.EmailAccount
	var credentialsChanged bool
	err := database.GetDB().WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", c.Param("id")).First(&account).Error; err != nil {
			return err
		}
		identityChanged := (req.Provider != "" && req.Provider != account.Provider) ||
			(req.Username != "" && req.Username != account.Username)
		credentialsChanged = identityChanged || req.Password != ""
		updates := make(map[string]interface{})
		if req.Email != "" {
			account.Email = req.Email
			updates["email"] = req.Email
		}
		if req.Provider != "" && req.Provider != account.Provider {
			provider := imap.ProviderConfigs[req.Provider]
			account.Provider = req.Provider
			account.IMAPHost, account.IMAPPort = provider.IMAPHost, provider.IMAPPort
			account.SMTPHost, account.SMTPPort = provider.SMTPHost, provider.SMTPPort
			updates["provider"], updates["imap_host"], updates["imap_port"] = account.Provider, account.IMAPHost, account.IMAPPort
			updates["smtp_host"], updates["smtp_port"] = account.SMTPHost, account.SMTPPort
		}
		if req.Username != "" {
			account.Username = req.Username
			updates["username"] = req.Username
		}
		if sealed != "" {
			account.Password = sealed
			updates["password"] = sealed
		}
		if req.Color != "" {
			account.Color = req.Color
			updates["color"] = req.Color
		}
		if credentialsChanged {
			account.ConfigVersion++
			updates["config_version"] = account.ConfigVersion
		}
		if identityChanged {
			account.IdentityVersion++
			account.UIDValidity = 0
			account.LastSyncTime = time.Time{}
			updates["identity_version"], updates["uid_validity"], updates["last_sync_time"] = account.IdentityVersion, 0, time.Time{}
			if err := tx.Where("account_id = ?", account.ID).Delete(&models.Email{}).Error; err != nil {
				return err
			}
		}
		if len(updates) == 0 {
			return nil
		}
		return tx.Model(&account).Updates(updates).Error
	})
	if err == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update account"})
		return
	}
	if credentialsChanged {
		imap.GetConnectionPool().RemoveConnection(account.ID, account.ConfigVersion)
	}
	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) TestAccount(c *gin.Context) {
	id := c.Param("id")

	var account models.EmailAccount
	if err := database.GetDB().Where("id = ?", id).First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	testClient := imap.NewClient(&account)
	if err := testClient.TestConnectionContext(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "connection successful"})
}
