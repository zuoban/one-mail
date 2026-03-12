package handlers

import (
	"net/http"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"

	"github.com/gin-gonic/gin"
)

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
