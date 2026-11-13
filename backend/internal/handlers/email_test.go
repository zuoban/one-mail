package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

func setupEmailHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "handler_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := database.SetupSQLite(db, true); err != nil {
		t.Fatalf("SetupSQLite failed: %v", err)
	}
	if err := database.MigrateAndOptimize(db); err != nil {
		t.Fatalf("MigrateAndOptimize failed: %v", err)
	}
	database.DB = db
	return db
}

func TestListEmails_CursorUnreadOwnershipAndSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	u1 := models.User{Username: "user1", Password: "p", Email: "u1@example.com"}
	u2 := models.User{Username: "user2", Password: "p", Email: "u2@example.com"}
	if err := db.Create(&u1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&u2).Error; err != nil {
		t.Fatal(err)
	}

	acc1 := models.EmailAccount{UserID: u1.ID, Email: "u1@example.com", Provider: "custom", IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "u1", Password: "p"}
	acc2 := models.EmailAccount{UserID: u2.ID, Email: "u2@example.com", Provider: "custom", IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "u2", Password: "p"}
	if err := db.Create(&acc1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&acc2).Error; err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	emails := []models.Email{
		{AccountID: acc1.ID, MessageID: "m1", From: "alice@acme.com", FromName: "Alice", To: "u1@example.com", Subject: "Weekly Report", Date: baseTime.Add(1 * time.Hour), IsRead: true, Folder: "INBOX", UID: 1},
		{AccountID: acc1.ID, MessageID: "m2", From: "bob@acme.com", FromName: "Bob", To: "u1@example.com", Subject: "电子发票通知", Date: baseTime.Add(2 * time.Hour), IsRead: false, Folder: "INBOX", UID: 2},
		{AccountID: acc1.ID, MessageID: "m3", From: "carol@acme.com", FromName: "Carol", To: "u1@example.com", Subject: "System Alert", Date: baseTime.Add(3 * time.Hour), IsRead: false, Folder: "INBOX", UID: 3},
		{AccountID: acc1.ID, MessageID: "m4", From: "dave@acme.com", FromName: "Dave", To: "u1@example.com", Subject: "项目周报", Date: baseTime.Add(4 * time.Hour), IsRead: true, Folder: "INBOX", UID: 4},
		{AccountID: acc2.ID, MessageID: "m5", From: "other@acme.com", FromName: "Other", To: "u2@example.com", Subject: "Secret Other User", Date: baseTime.Add(5 * time.Hour), IsRead: false, Folder: "INBOX", UID: 5},
	}
	if err := db.Create(&emails).Error; err != nil {
		t.Fatal(err)
	}

	handler := NewEmailHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Next()
	})
	r.GET("/emails", handler.ListEmails)

	type listResp struct {
		Data       []models.Email `json:"data"`
		Total      int64          `json:"total"`
		HasMore    bool           `json:"has_more"`
		NextCursor *EmailCursor   `json:"next_cursor"`
	}

	// 1. Cross-user account_id isolation check
	req := httptest.NewRequest(http.MethodGet, "/emails?account_id=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 0 || resp.Total != 0 {
		t.Fatalf("expected 0 emails for another user's account, got %d", len(resp.Data))
	}

	// 2. Cursor pagination across all pages (page_size=2)
	req = httptest.NewRequest(http.MethodGet, "/emails?page_size=2", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 || resp.Total != 4 || !resp.HasMore || resp.NextCursor == nil {
		t.Fatalf("unexpected page 1 response: len=%d total=%d hasMore=%v cursor=%v", len(resp.Data), resp.Total, resp.HasMore, resp.NextCursor)
	}
	if resp.Data[0].Subject != "项目周报" || resp.Data[1].Subject != "System Alert" {
		t.Fatalf("unexpected page 1 order: %q, %q", resp.Data[0].Subject, resp.Data[1].Subject)
	}

	req = httptest.NewRequest(http.MethodGet, "/emails?page_size=2&cursor_id=3&cursor_date="+resp.NextCursor.Date.Format(time.RFC3339Nano), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp2 listResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if len(resp2.Data) != 2 || resp2.HasMore || resp2.Total != 0 {
		t.Fatalf("unexpected cursor page 2: len=%d hasMore=%v total=%d", len(resp2.Data), resp2.HasMore, resp2.Total)
	}
	if resp2.Data[0].Subject != "电子发票通知" || resp2.Data[1].Subject != "Weekly Report" {
		t.Fatalf("unexpected page 2 order: %q, %q", resp2.Data[0].Subject, resp2.Data[1].Subject)
	}

	// 3. Unread filter
	req = httptest.NewRequest(http.MethodGet, "/emails?unread=true", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var unreadResp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &unreadResp); err != nil {
		t.Fatal(err)
	}
	if len(unreadResp.Data) != 2 || unreadResp.Total != 2 {
		t.Fatalf("expected 2 unread emails, got %d (total=%d)", len(unreadResp.Data), unreadResp.Total)
	}

	// 4. Search filter (tests FTS5 trigram if enabled, or LIKE fallback)
	req = httptest.NewRequest(http.MethodGet, "/emails?search=%E5%8F%91%E7%A5%A8", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var searchResp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &searchResp); err != nil {
		t.Fatal(err)
	}
	if len(searchResp.Data) != 1 || searchResp.Data[0].Subject != "电子发票通知" {
		t.Fatalf("expected 1 search result for 发票, got %+v", searchResp.Data)
	}

	// 5. Single-page total shortcut and out-of-range page fallback
	req = httptest.NewRequest(http.MethodGet, "/emails?page=1&page_size=10", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var singleResp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &singleResp); err != nil {
		t.Fatal(err)
	}
	if len(singleResp.Data) != 4 || singleResp.Total != 4 || singleResp.HasMore {
		t.Fatalf("expected single-page total=4 hasMore=false, got len=%d total=%d hasMore=%v", len(singleResp.Data), singleResp.Total, singleResp.HasMore)
	}

	req = httptest.NewRequest(http.MethodGet, "/emails?page=3&page_size=10", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var emptyPageResp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &emptyPageResp); err != nil {
		t.Fatal(err)
	}
	if len(emptyPageResp.Data) != 0 || emptyPageResp.Total != 4 || emptyPageResp.HasMore {
		t.Fatalf("expected empty page 3 with total=4, got len=%d total=%d hasMore=%v", len(emptyPageResp.Data), emptyPageResp.Total, emptyPageResp.HasMore)
	}
}
