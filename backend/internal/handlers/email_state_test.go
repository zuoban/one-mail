package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"one-mail/backend/internal/models"

	"github.com/gin-gonic/gin"
)

func TestReadStateIsDurableWhenRemoteSyncFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "owner@example.com", IMAPHost: "127.0.0.1", IMAPPort: 1, Username: "owner", Password: "temporary", UIDValidity: 11}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "state", UID: 1, UIDValidity: 11, IsRead: true}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	h := NewEmailHandler()
	r := gin.New()
	r.POST("/emails/:id/read", h.MarkAsRead)
	r.POST("/emails/:id/unread", h.MarkAsUnread)
	r.POST("/emails/batch/read", h.BatchMarkAsRead)
	for _, operation := range []struct {
		path string
		body string
		seen bool
	}{
		{fmt.Sprintf("/emails/%d/unread", email.ID), "", false},
		{fmt.Sprintf("/emails/%d/read", email.ID), "", true},
		{fmt.Sprintf("/emails/%d/unread", email.ID), "", false},
		{"/emails/batch/read", fmt.Sprintf(`{"ids":[%d]}`, email.ID), true},
	} {
		req := httptest.NewRequest(http.MethodPost, operation.path, strings.NewReader(operation.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("read state failed: status=%d body=%s", w.Code, w.Body.String())
		}
		var response struct {
			SyncPending bool `json:"sync_pending"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.SyncPending {
			t.Fatal("remote failure must be reported as pending")
		}
		var stored models.Email
		if err := db.First(&stored, email.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.IsRead != operation.seen || stored.PendingRead == nil || *stored.PendingRead != operation.seen {
			t.Fatal("user state must survive a failed remote write")
		}
	}
}

func TestDeletedAccountEmailsAreInaccessible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "orphan", BodyText: "cached private body"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate an account deleted by an older version that left cached messages.
	if err := db.Delete(&account).Error; err != nil {
		t.Fatal(err)
	}
	h := NewEmailHandler()
	r := gin.New()
	r.GET("/emails/:id", h.GetEmail)
	r.POST("/emails/:id/read", h.MarkAsRead)
	r.POST("/emails/:id/unread", h.MarkAsUnread)
	r.DELETE("/emails/:id", h.DeleteEmail)
	for _, operation := range []struct{ method, suffix string }{
		{http.MethodGet, ""},
		{http.MethodPost, "/read"},
		{http.MethodPost, "/unread"},
		{http.MethodDelete, ""},
	} {
		path := fmt.Sprintf("/emails/%d%s", email.ID, operation.suffix)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(operation.method, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("deleted account email accessible via %s %s: %d", operation.method, operation.suffix, w.Code)
		}
	}
	r.GET("/emails", h.ListEmails)
	r.POST("/emails/batch/read", h.BatchMarkAsRead)
	list := httptest.NewRecorder()
	r.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/emails", nil))
	var listed struct {
		Data []models.Email `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if list.Code != http.StatusOK || len(listed.Data) != 0 {
		t.Fatalf("deleted mailbox appeared in list: %d %s", list.Code, list.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/emails/batch/read", strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, email.ID)))
	req.Header.Set("Content-Type", "application/json")
	batch := httptest.NewRecorder()
	r.ServeHTTP(batch, req)
	var response struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if batch.Code != http.StatusOK || response.Count != 0 {
		t.Fatalf("batch modified deleted mailbox: %d %s", batch.Code, batch.Body.String())
	}
	var stored models.Email
	if err := db.First(&stored, email.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.IsRead || stored.PendingRead != nil || stored.DeletedAt.Valid {
		t.Fatal("inaccessible mailbox message was modified")
	}
}

func TestMissingAndDeletedEmailsRemainUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "mailbox@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "removed-message", BodyText: "cached body"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&email).Error; err != nil {
		t.Fatal(err)
	}
	h := NewEmailHandler()
	r := gin.New()
	r.GET("/emails/:id", h.GetEmail)
	r.POST("/emails/:id/read", h.MarkAsRead)
	r.POST("/emails/:id/unread", h.MarkAsUnread)
	r.POST("/emails/batch/read", h.BatchMarkAsRead)
	r.DELETE("/emails/:id", h.DeleteEmail)
	for _, id := range []uint{email.ID, email.ID + 100} {
		for _, operation := range []struct{ method, suffix string }{
			{http.MethodGet, ""},
			{http.MethodPost, "/read"},
			{http.MethodPost, "/unread"},
			{http.MethodDelete, ""},
		} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(operation.method, fmt.Sprintf("/emails/%d%s", id, operation.suffix), nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("unavailable message %d accessible via %s %s: %d", id, operation.method, operation.suffix, w.Code)
			}
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/emails/batch/read", strings.NewReader(fmt.Sprintf(`{"ids":[%d,%d]}`, email.ID, email.ID+100)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var response struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Count != 0 {
		t.Fatalf("batch included unavailable messages: %d %s", w.Code, w.Body.String())
	}
	var stored models.Email
	if err := db.Unscoped().First(&stored, email.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.DeletedAt.Valid || stored.IsRead || stored.PendingRead != nil {
		t.Fatal("batch modified a deleted message")
	}
}
