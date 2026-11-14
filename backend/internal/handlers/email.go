package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type EmailHandler struct{}

func NewEmailHandler() *EmailHandler {
	return &EmailHandler{}
}

type ListEmailsQuery struct {
	AccountID uint   `form:"account_id"`
	Folder    string `form:"folder"`
	Search    string `form:"search"`
	Unread    bool   `form:"unread"`
}

func (h *EmailHandler) ListEmails(c *gin.Context) {
	var query ListEmailsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email query"})
		return
	}

	db := database.GetDB()

	var accounts []models.EmailAccount
	accountQuery := db.Select("id")
	if query.AccountID > 0 {
		accountQuery = accountQuery.Where("id = ?", query.AccountID)
	}
	if err := accountQuery.Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load accounts"})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"data":  []interface{}{},
			"total": 0,
		})
		return
	}

	accountIDs := make([]uint, 0, len(accounts))
	for _, a := range accounts {
		accountIDs = append(accountIDs, a.ID)
	}

	q := db.Model(&models.Email{}).Where("account_id IN ?", accountIDs)

	folder := query.Folder
	if folder == "" {
		folder = mail.DefaultFolder
	}
	q = q.Where("folder = ?", folder)

	if query.Unread {
		q = q.Where("is_read = ?", false)
	}

	search := strings.TrimSpace(query.Search)
	if search != "" {
		if database.IsFTS5Enabled() && utf8.RuneCountInString(search) >= 3 {
			escaped := strings.ReplaceAll(search, `"`, `""`)
			ftsExpr := `"` + escaped + `"`
			q = q.Where("emails.id IN (SELECT rowid FROM emails_fts WHERE emails_fts MATCH ?)", ftsExpr)
		} else {
			pattern := "%" + search + "%"
			q = q.Where("(subject LIKE ? OR `from` LIKE ? OR from_name LIKE ?)", pattern, pattern, pattern)
		}
	}

	type EmailListItem struct {
		ID            uint      `json:"id"`
		CreatedAt     time.Time `json:"created_at"`
		AccountID     uint      `json:"account_id"`
		From          string    `json:"from"`
		FromName      string    `json:"from_name"`
		To            string    `json:"to"`
		Subject       string    `json:"subject"`
		Date          time.Time `json:"date"`
		HasAttachment bool      `json:"has_attachment"`
		IsRead        bool      `json:"is_read"`
		Folder        string    `json:"folder"`
	}

	var emails []EmailListItem
	if err := q.
		Select("id, created_at, account_id, `from`, from_name, `to`, subject, date, has_attachment, is_read, folder").
		Order("date DESC, id DESC").
		Limit(mail.MaxLatestEmails).
		Find(&emails).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  emails,
		"total": len(emails),
	})
}

func (h *EmailHandler) GetEmail(c *gin.Context) {
	id := c.Param("id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.deleted_at IS NULL", id).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}
	var ownerAccount models.EmailAccount
	if err := database.GetDB().First(&ownerAccount, email.AccountID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if ownerAccount.UIDValidity != 0 && email.UIDValidity != ownerAccount.UIDValidity {
		c.JSON(http.StatusConflict, gin.H{"error": "mailbox identity changed; refresh the email list before retrying"})
		return
	}

	if !email.BodyFetched && email.BodyText == "" && email.BodyHTML == "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if email.UIDValidity == 0 {
			var account models.EmailAccount
			if err := database.GetDB().First(&account, email.AccountID).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
				return
			}
			if _, err := mail.RefreshAccount(ctx, database.GetDB(), &account, mail.MaxLatestEmails); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "unable to refresh mailbox identity; retry loading the message"})
				return
			}
			if err := database.GetDB().First(&email, email.ID).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "message changed; refresh the email list"})
				return
			}
		}
		if err := h.fetchAndCacheBody(ctx, &email); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load email body; please retry"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": email})
}

func (h *EmailHandler) fetchAndCacheBody(ctx context.Context, email *models.Email) error {
	var account models.EmailAccount
	if err := database.GetDB().First(&account, email.AccountID).Error; err != nil {
		return err
	}
	imapClient, release, err := imap.GetConnectionPool().AcquireConnectionContext(ctx, &account)
	if err != nil {
		return err
	}
	bodyText, bodyHTML, err := imapClient.FetchEmailBody(ctx, email.Folder, email.UID, email.UIDValidity)
	release(err == nil)
	if err != nil {
		return err
	}
	result := database.GetDB().WithContext(ctx).Model(&models.Email{}).
		Where("id = ? AND uid = ? AND uid_validity = ?", email.ID, email.UID, email.UIDValidity).
		Updates(map[string]interface{}{"body_text": bodyText, "body_html": bodyHTML, "body_fetched": true})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	email.BodyText, email.BodyHTML, email.BodyFetched = bodyText, bodyHTML, true
	return nil
}

func (h *EmailHandler) MarkAsRead(c *gin.Context) {
	h.setReadState(c, true)
}

func (h *EmailHandler) MarkAsUnread(c *gin.Context) {
	h.setReadState(c, false)
}

func (h *EmailHandler) setReadState(c *gin.Context, seen bool) {
	var email models.Email
	db := database.GetDB()
	if err := db.Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.deleted_at IS NULL", c.Param("id")).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}
	identified := []models.Email{email}
	if err := refreshReadIdentities(c.Request.Context(), db, identified); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "mailbox identity changed; refresh the email list before retrying"})
		return
	}
	email = identified[0]
	if err := db.Model(&email).Updates(map[string]interface{}{"is_read": seen, "pending_read": seen}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save read state"})
		return
	}
	syncPendingRead(c.Request.Context(), db, []uint{email.AccountID})
	var pending int64
	if err := db.Model(&models.Email{}).Where("id = ? AND pending_read IS NOT NULL", email.ID).Count(&pending).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load sync status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "read state saved", "sync_pending": pending > 0})
}

// The desired state is durable before contacting IMAP; a failed attempt remains
// pending and is retried by the next refresh rather than silently discarded.
func syncPendingRead(ctx context.Context, db *gorm.DB, accountIDs []uint) {
	syncCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, id := range accountIDs {
		if syncCtx.Err() != nil {
			return
		}
		var account models.EmailAccount
		if err := db.First(&account, id).Error; err != nil {
			continue
		}
		_ = mail.SyncPendingRead(syncCtx, db, &account)
	}
}

func refreshReadIdentities(ctx context.Context, db *gorm.DB, emails []models.Email) error {
	refreshed := make(map[uint]bool)
	for _, email := range emails {
		if email.UIDValidity != 0 || refreshed[email.AccountID] {
			continue
		}
		var account models.EmailAccount
		if err := db.First(&account, email.AccountID).Error; err != nil {
			return err
		}
		if _, err := mail.RefreshAccount(ctx, db, &account, mail.MaxLatestEmails); err != nil {
			return err
		}
		refreshed[email.AccountID] = true
	}
	for i := range emails {
		if !refreshed[emails[i].AccountID] {
			continue
		}
		if err := db.First(&emails[i], emails[i].ID).Error; err != nil {
			return err
		}
		if emails[i].UIDValidity == 0 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func (h *EmailHandler) BatchMarkAsRead(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required,min=1,max=1000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var emails []models.Email
	db := database.GetDB()
	if err := db.Select("emails.id, emails.account_id, emails.uid_validity").
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id IN ? AND email_accounts.deleted_at IS NULL", req.IDs).
		Find(&emails).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load emails"})
		return
	}
	if err := refreshReadIdentities(c.Request.Context(), db, emails); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "mailbox identity changed; refresh the email list before retrying"})
		return
	}
	validIDs := make([]uint, 0, len(emails))
	accountIDs := make([]uint, 0)
	seenAccounts := make(map[uint]bool)
	for _, email := range emails {
		validIDs = append(validIDs, email.ID)
		if !seenAccounts[email.AccountID] {
			seenAccounts[email.AccountID] = true
			accountIDs = append(accountIDs, email.AccountID)
		}
	}

	if len(validIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "marked as read", "count": 0})
		return
	}

	result := db.Model(&models.Email{}).Where("id IN ?", validIDs).
		Updates(map[string]interface{}{"is_read": true, "pending_read": true})

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	syncPendingRead(c.Request.Context(), db, accountIDs)
	var pending int64
	if err := db.Model(&models.Email{}).Where("id IN ? AND pending_read IS NOT NULL", validIDs).Count(&pending).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load sync status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "read state saved", "count": result.RowsAffected, "sync_pending": pending > 0})
}

func (h *EmailHandler) DeleteEmail(c *gin.Context) {
	id := c.Param("id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.deleted_at IS NULL", id).
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

func (h *EmailHandler) RefreshEmails(c *gin.Context) {
	var query struct {
		AccountID uint `form:"account_id"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid refresh query"})
		return
	}

	db := database.GetDB()

	var accounts []models.EmailAccount
	q := db.Where("status = ?", "active")
	if query.AccountID > 0 {
		q = q.Where("id = ?", query.AccountID)
	}
	if err := q.Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(accounts) == 0 {
		if query.AccountID > 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":            "no active accounts",
			"refreshed_accounts": 0,
			"new_count":          0,
		})
		return
	}

	summary := mail.RefreshAccounts(c.Request.Context(), db, accounts, mail.MaxLatestEmails)
	if summary.Refreshed == 0 && len(summary.Errors) > 0 {
		for _, errMsg := range summary.Errors {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":              errMsg,
				"refreshed_accounts": 0,
				"new_count":          0,
				"errors":             summary.Errors,
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":            "refreshed",
		"refreshed_accounts": summary.Refreshed,
		"new_count":          summary.NewCount,
		"errors":             summary.Errors,
	})
}

func (h *EmailHandler) RefreshAccountEmails(c *gin.Context) {
	accountID := c.Param("id")
	db := database.GetDB()

	var account models.EmailAccount
	if err := db.Where("id = ?", accountID).First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	newCount, err := mail.RefreshAccount(c.Request.Context(), db, &account, mail.MaxLatestEmails)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "refreshed",
		"new_count": newCount,
	})
}
