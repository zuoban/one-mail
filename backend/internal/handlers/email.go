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

	"github.com/gin-gonic/gin"
)

type EmailHandler struct{}

func NewEmailHandler() *EmailHandler {
	return &EmailHandler{}
}

type ListEmailsQuery struct {
	AccountID  uint   `form:"account_id"`
	Folder     string `form:"folder"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
	Search     string `form:"search"`
	Unread     bool   `form:"unread"`
	CursorDate string `form:"cursor_date"`
	CursorID   uint   `form:"cursor_id"`
}

type EmailCursor struct {
	Date time.Time `json:"date"`
	ID   uint      `json:"id"`
}

func (h *EmailHandler) ListEmails(c *gin.Context) {
	var query ListEmailsQuery
	_ = c.ShouldBindQuery(&query)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}

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
			"data":        []interface{}{},
			"total":       0,
			"has_more":    false,
			"next_cursor": nil,
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
		folder = "INBOX"
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

	useCursor := query.CursorID > 0 && query.CursorDate != ""
	var total int64
	if !useCursor {
		q.Count(&total)
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

	listQuery := q.Select("id, created_at, account_id, `from`, from_name, `to`, subject, date, has_attachment, is_read, folder").
		Order("date DESC, id DESC")

	var emails []EmailListItem
	hasMore := false
	var nextCursor *EmailCursor

	if useCursor {
		cursorTime, err := time.Parse(time.RFC3339Nano, query.CursorDate)
		if err != nil {
			cursorTime, err = time.Parse(time.RFC3339, query.CursorDate)
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor_date"})
			return
		}
		listQuery = listQuery.Where("(date < ? OR (date = ? AND id < ?))", cursorTime, cursorTime, query.CursorID).
			Limit(query.PageSize + 1)
		if err := listQuery.Find(&emails).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(emails) > query.PageSize {
			hasMore = true
			emails = emails[:query.PageSize]
		}
	} else {
		offset := (query.Page - 1) * query.PageSize
		if err := listQuery.Offset(offset).Limit(query.PageSize).Find(&emails).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hasMore = int64(offset+len(emails)) < total
	}

	if len(emails) > 0 && hasMore {
		last := emails[len(emails)-1]
		nextCursor = &EmailCursor{
			Date: last.Date,
			ID:   last.ID,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        emails,
		"total":       total,
		"page":        query.Page,
		"page_size":   query.PageSize,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
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
	imapClient, release, err := pool.AcquireConnection(&account)
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

	go func() {
		var account models.EmailAccount
		if err := database.GetDB().First(&account, email.AccountID).Error; err != nil {
			return
		}

		imapClient, release, err := imap.GetConnectionPool().AcquireConnection(&account)
		if err != nil {
			return
		}
		healthy := true
		defer func() {
			release(healthy)
		}()

		if err := imapClient.MarkAsRead(email.UID, email.Folder); err != nil {
			healthy = false
		}
	}()
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

	imapClient, release, err := imap.GetConnectionPool().AcquireConnection(&account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	healthy := true
	defer func() {
		release(healthy)
	}()

	emails, err := imapClient.FetchEmails("INBOX", account.LastSyncTime, 50)
	if err != nil {
		healthy = false
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 批量查询已存在的 message_id，避免 N+1 查询
	var existingMessageIDs []string
	messageIDs := make([]string, 0, len(emails))
	for _, e := range emails {
		messageIDs = append(messageIDs, e.MessageID)
	}

	if len(messageIDs) > 0 {
		database.GetDB().Model(&models.Email{}).
			Where("account_id = ? AND message_id IN ?", account.ID, messageIDs).
			Pluck("message_id", &existingMessageIDs)
	}

	// 创建已存在 message_id 的集合，用于快速查找
	existingSet := make(map[string]bool, len(existingMessageIDs))
	for _, id := range existingMessageIDs {
		existingSet[id] = true
	}

	var newEmails []models.Email
	for _, e := range emails {
		// 使用内存查找替代数据库查询
		if existingSet[e.MessageID] {
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
