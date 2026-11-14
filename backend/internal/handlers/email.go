package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"github.com/gin-gonic/gin"
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
	_ = c.ShouldBindQuery(&query)

	userID := c.GetUint("user_id")
	db := database.GetDB()

	var accounts []models.EmailAccount
	if query.AccountID > 0 {
		db.Select("id").Where("id = ? AND user_id = ?", query.AccountID, userID).Find(&accounts)
	} else {
		db.Select("id").Where("user_id = ?", userID).Find(&accounts)
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
	userID := c.GetUint("user_id")

	var email models.Email
	if err := database.GetDB().
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id = ? AND email_accounts.user_id = ?", id, userID).
		First(&email).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "email not found"})
		return
	}

	if email.BodyText == "" && email.BodyHTML == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.fetchAndCacheBody(ctx, &email)
	}

	c.JSON(http.StatusOK, gin.H{"data": email})
}

func (h *EmailHandler) fetchAndCacheBody(ctx context.Context, email *models.Email) {
	var account models.EmailAccount
	if err := database.GetDB().First(&account, email.AccountID).Error; err != nil {
		return
	}

	pool := imap.GetConnectionPool()
	imapClient, release, err := pool.AcquireConnectionContext(ctx, &account)
	if err != nil {
		return
	}
	healthy := true
	defer func() {
		release(healthy)
	}()

	type fetchResult struct {
		bodyText string
		bodyHTML string
		err      error
	}
	done := make(chan fetchResult, 1)
	go func() {
		bt, bh, fetchErr := imapClient.FetchEmailBody(email.Folder, email.UID)
		done <- fetchResult{bodyText: bt, bodyHTML: bh, err: fetchErr}
	}()

	var res fetchResult
	select {
	case res = <-done:
	case <-ctx.Done():
		healthy = false
		imapClient.ForceClose()
		return
	}
	if res.err != nil {
		healthy = false
		return
	}

	if res.bodyText != "" || res.bodyHTML != "" {
		database.GetDB().Model(email).Updates(map[string]interface{}{
			"body_text": res.bodyText,
			"body_html": res.bodyHTML,
		})
	}

	email.BodyText = res.bodyText
	email.BodyHTML = res.bodyHTML
}

type markReadJob struct {
	accountID uint
	uid       uint
	folder    string
}

var (
	markReadQueue     chan markReadJob
	markReadQueueOnce sync.Once
)

func enqueueMarkAsRead(accountID, uid uint, folder string) {
	markReadQueueOnce.Do(func() {
		markReadQueue = make(chan markReadJob, 256)
		for range 2 {
			go func() {
				for job := range markReadQueue {
					var account models.EmailAccount
					if err := database.GetDB().First(&account, job.accountID).Error; err != nil {
						continue
					}

					imapClient, release, err := imap.GetConnectionPool().AcquireConnection(&account)
					if err != nil {
						continue
					}
					healthy := true
					if err := imapClient.MarkAsRead(job.uid, job.folder); err != nil {
						healthy = false
					}
					release(healthy)
				}
			}()
		}
	})

	select {
	case markReadQueue <- markReadJob{accountID: accountID, uid: uid, folder: folder}:
	default:
	}
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

	enqueueMarkAsRead(email.AccountID, email.UID, email.Folder)
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

func (h *EmailHandler) BatchMarkAsRead(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetUint("user_id")

	var validIDs []uint
	database.GetDB().
		Table("emails").
		Select("emails.id").
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id").
		Where("emails.id IN ? AND email_accounts.user_id = ?", req.IDs, userID).
		Pluck("id", &validIDs)

	if len(validIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "marked as read", "count": 0})
		return
	}

	result := database.GetDB().Model(&models.Email{}).Where("id IN ?", validIDs).Update("is_read", true)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "marked as read", "count": result.RowsAffected})
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

func (h *EmailHandler) RefreshEmails(c *gin.Context) {
	var query struct {
		AccountID uint `form:"account_id"`
	}
	_ = c.ShouldBindQuery(&query)

	userID := c.GetUint("user_id")
	db := database.GetDB()

	var accounts []models.EmailAccount
	q := db.Where("user_id = ? AND status = ?", userID, "active")
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
	userID := c.GetUint("user_id")
	db := database.GetDB()

	var account models.EmailAccount
	if err := db.Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
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
