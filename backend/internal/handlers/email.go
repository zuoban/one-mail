package handlers

import (
	"net/http"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"

	"github.com/gin-gonic/gin"
)

type EmailHandler struct{}

func NewEmailHandler() *EmailHandler {
	return &EmailHandler{}
}

type ListEmailsQuery struct {
	AccountID uint   `form:"account_id"`
	Folder    string `form:"folder"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
	Search    string `form:"search"`
	Unread    bool   `form:"unread"`
}

func (h *EmailHandler) ListEmails(c *gin.Context) {
	var query ListEmailsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		query.Page = 1
		query.PageSize = 20
	}

	userID := c.GetUint("user_id")

	db := database.GetDB()

	var accounts []models.EmailAccount
	if query.AccountID > 0 {
		accounts = []models.EmailAccount{{ID: query.AccountID}}
	} else {
		db.Where("user_id = ?", userID).Find(&accounts)
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}, "total": 0})
		return
	}

	var accountIDs []uint
	for _, a := range accounts {
		accountIDs = append(accountIDs, a.ID)
	}

	db = db.Where("account_id IN ?", accountIDs)

	if query.Search != "" {
		db = db.Where("subject LIKE ? OR `from` LIKE ?", "%"+query.Search+"%", "%"+query.Search+"%")
	}

	if query.Unread {
		db = db.Where("is_read = ?", false)
	}

	if query.Folder != "" {
		db = db.Where("folder = ?", query.Folder)
	} else {
		db = db.Where("folder = ?", "INBOX")
	}

	var total int64
	db.Model(&models.Email{}).Count(&total)

	offset := (query.Page - 1) * query.PageSize

	var emails []models.Email
	if err := db.Order("date DESC").Offset(offset).Limit(query.PageSize).Find(&emails).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": emails, "total": total, "page": query.Page, "page_size": query.PageSize})
}

func (h *EmailHandler) GetEmail(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetUint("user_id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.user_id = ?", id, userID).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	if !email.IsRead {
		database.GetDB().Model(&email).Update("is_read", true)
	}

	if email.BodyText == "" && email.BodyHTML == "" {
		h.fetchAndCacheBody(&email)
	}

	c.JSON(http.StatusOK, gin.H{"data": email})
}

func (h *EmailHandler) fetchAndCacheBody(email *models.Email) {
	var account models.EmailAccount
	if err := database.GetDB().First(&account, email.AccountID).Error; err != nil {
		return
	}

	imapClient := imap.NewClient(&account)
	if err := imapClient.Connect(); err != nil {
		return
	}
	defer imapClient.Disconnect()

	bodyText, bodyHTML, err := imapClient.FetchEmailBody(email.Folder, email.UID)
	if err != nil {
		return
	}

	database.GetDB().Model(email).Updates(map[string]interface{}{
		"body_text": bodyText,
		"body_html": bodyHTML,
	})

	email.BodyText = bodyText
	email.BodyHTML = bodyHTML
}

func (h *EmailHandler) MarkAsRead(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetUint("user_id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.user_id = ?", id, userID).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	database.GetDB().Model(&email).Update("is_read", true)

	var account models.EmailAccount
	database.GetDB().First(&account, email.AccountID)

	imapClient := imap.NewClient(&account)
	if err := imapClient.Connect(); err == nil {
		imapClient.MarkAsRead(email.UID, email.Folder)
		imapClient.Disconnect()
	}

	c.JSON(http.StatusOK, gin.H{"message": "marked as read"})
}

func (h *EmailHandler) MarkAsUnread(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetUint("user_id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.user_id = ?", id, userID).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	database.GetDB().Model(&email).Update("is_read", false)

	c.JSON(http.StatusOK, gin.H{"message": "marked as unread"})
}

func (h *EmailHandler) DeleteEmail(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetUint("user_id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.user_id = ?", id, userID).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	if err := database.GetDB().Delete(&email).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *EmailHandler) SyncEmails(c *gin.Context) {
	accountID := c.Param("id")
	userID := c.GetUint("user_id")

	var account models.EmailAccount
	if err := database.GetDB().
		Where("id = ? AND user_id = ?", accountID, userID).
		First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	imapClient := imap.NewClient(&account)
	if err := imapClient.Connect(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer imapClient.Disconnect()

	emails, err := imapClient.FetchEmails("INBOX", account.LastSyncTime, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var newEmails []models.Email
	for _, e := range emails {
		var existing models.Email
		result := database.GetDB().Where("account_id = ? AND message_id = ?", account.ID, e.MessageID).First(&existing)
		if result.Error == nil {
			continue
		}

		email := models.Email{
			AccountID:     account.ID,
			MessageID:     e.MessageID,
			From:          e.From,
			FromName:      e.FromName,
			To:            e.To,
			Subject:       e.Subject,
			Date:          e.Date,
			BodyText:      e.BodyText,
			BodyHTML:      e.BodyHTML,
			HasAttachment: e.HasAttachment,
			IsRead:        false,
			Folder:        "INBOX",
			UID:           uint(e.UID),
		}
		newEmails = append(newEmails, email)
	}

	if len(newEmails) > 0 {
		database.GetDB().Create(&newEmails)
	}

	database.GetDB().Model(&account).Update("last_sync_time", time.Now())

	c.JSON(http.StatusOK, gin.H{"message": "synced", "count": len(newEmails)})
}
