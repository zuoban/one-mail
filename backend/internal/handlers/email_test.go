package handlers

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/application"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
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
	if err := database.InitializeSchema(db); err != nil {
		t.Fatalf("InitializeSchema failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func newTestMailService(t *testing.T, db *gorm.DB) (*application.MailService, *imap.ConnectionPool) {
	t.Helper()
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	return application.NewMailService(db, pool, false), pool
}

func newTestEmailHandler(t *testing.T, db *gorm.DB) *EmailHandler {
	service, _ := newTestMailService(t, db)
	return NewEmailHandler(service)
}

func newTestAccountHandler(t *testing.T, db *gorm.DB) *AccountHandler {
	service, pool := newTestMailService(t, db)
	return NewAccountHandler(application.NewAccountService(db, pool, service))
}

func TestListEmails_Latest50SharedAccountsAndSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	acc1 := models.EmailAccount{Email: "u1@example.com", Provider: "custom", IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "u1", Password: "p"}
	acc2 := models.EmailAccount{Email: "u2@example.com", Provider: "custom", IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "u2", Password: "p"}
	if err := db.Create(&acc1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&acc2).Error; err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	emails := []models.Email{
		{AccountID: acc1.ID, MessageID: "m1", From: "alice@acme.com", FromName: "Alice", To: "u1@example.com", Subject: "Weekly Report", Date: baseTime.Add(1 * time.Hour), Folder: "INBOX", UID: 1},
		{AccountID: acc1.ID, MessageID: "m2", From: "bob@acme.com", FromName: "Bob", To: "u1@example.com", Subject: "电子发票通知", Date: baseTime.Add(2 * time.Hour), Folder: "INBOX", UID: 2},
		{AccountID: acc1.ID, MessageID: "m3", From: "carol@acme.com", FromName: "Carol", To: "u1@example.com", Subject: "System Alert", Date: baseTime.Add(3 * time.Hour), Folder: "INBOX", UID: 3},
		{AccountID: acc1.ID, MessageID: "m4", From: "dave@acme.com", FromName: "Dave", To: "u1@example.com", Subject: "项目周报", Date: baseTime.Add(4 * time.Hour), Folder: "INBOX", UID: 4},
		{AccountID: acc2.ID, MessageID: "m5", From: "other@acme.com", FromName: "Other", To: "u2@example.com", Subject: "Second Mailbox", Date: baseTime.Add(5 * time.Hour), Folder: "INBOX", UID: 5},
	}
	if err := db.Create(&emails).Error; err != nil {
		t.Fatal(err)
	}

	handler := newTestEmailHandler(t, db)
	r := gin.New()
	r.GET("/emails", handler.ListEmails)

	type listResp struct {
		Data  []models.Email `json:"data"`
		Total int            `json:"total"`
	}

	// An explicit account filter includes that mailbox without an application user.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails?account_id=%d", acc2.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Total != 1 || resp.Data[0].AccountID != acc2.ID {
		t.Fatalf("expected the selected mailbox email, got %+v", resp)
	}

	// All configured mailboxes are included, ordered newest first.
	req = httptest.NewRequest(http.MethodGet, "/emails", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 5 || resp.Total != 5 {
		t.Fatalf("unexpected response: len=%d total=%d", len(resp.Data), resp.Total)
	}
	if resp.Data[0].Subject != "Second Mailbox" || resp.Data[1].Subject != "项目周报" || resp.Data[2].Subject != "System Alert" || resp.Data[3].Subject != "电子发票通知" || resp.Data[4].Subject != "Weekly Report" {
		t.Fatalf("unexpected order: %+v", resp.Data)
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

	// Omitting the count keeps the default window at 50.
	extra := make([]models.Email, 0, 55)
	for i := 1; i <= 55; i++ {
		extra = append(extra, models.Email{
			AccountID: acc1.ID,
			MessageID: fmt.Sprintf("extra-%d", i),
			From:      "bulk@acme.com",
			To:        "u1@example.com",
			Subject:   fmt.Sprintf("Bulk %d", i),
			Date:      baseTime.Add(time.Duration(10+i) * time.Hour),
			Folder:    "INBOX",
			UID:       uint(100 + i),
		})
	}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/emails", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var cappedResp listResp
	if err := json.Unmarshal(w.Body.Bytes(), &cappedResp); err != nil {
		t.Fatal(err)
	}
	if len(cappedResp.Data) != 50 || cappedResp.Total != 50 {
		t.Fatalf("expected capped at 50 emails, got len=%d total=%d", len(cappedResp.Data), cappedResp.Total)
	}
	if cappedResp.Data[0].Subject != "Bulk 55" || cappedResp.Data[49].Subject != "Bulk 6" {
		t.Fatalf("expected Bulk 55..Bulk 6, got first=%q last=%q", cappedResp.Data[0].Subject, cappedResp.Data[49].Subject)
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"limit=0", 50},
		{"limit=25", 25},
		{"limit=100", 60},
		{"limit=200", 60},
		{fmt.Sprintf("limit=100&account_id=%d", acc1.ID), 59},
		{fmt.Sprintf("limit=100&account_id=%d&search=Bulk", acc1.ID), 55},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/emails?"+tc.query, nil))
		var result listResp
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || len(result.Data) != tc.want || result.Total != tc.want {
			t.Fatalf("%s: status=%d result=%+v", tc.query, w.Code, result)
		}
	}
	var remaining int64
	if err := db.Model(&models.Email{}).Count(&remaining).Error; err != nil || remaining != 60 {
		t.Fatalf("list changed cached window: count=%d err=%v", remaining, err)
	}
}

func TestEmailEndpointsRejectInvalidLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newTestEmailHandler(t, setupEmailHandlerTestDB(t))
	r := gin.New()
	r.GET("/emails", handler.ListEmails)
	r.POST("/emails/refresh", handler.RefreshEmails)
	r.POST("/accounts/:id/refresh", handler.RefreshAccountEmails)
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/emails"},
		{http.MethodPost, "/emails/refresh"},
		{http.MethodPost, "/accounts/1/refresh"},
	} {
		for _, limit := range []string{"-1", "201", "1000", "abc", "1.5", "99999999999999999999999999999999"} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path+"?limit="+limit, nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s %s limit=%s: status=%d body=%s", endpoint.method, endpoint.path, limit, w.Code, w.Body.String())
			}
		}
	}
}
