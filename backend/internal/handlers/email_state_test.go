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
	user := models.User{Username: "owner"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{UserID: user.ID, Email: "owner@example.com", IMAPHost: "127.0.0.1", IMAPPort: 1, Username: "owner", Password: "temporary", UIDValidity: 11}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "state", UID: 1, UIDValidity: 11, IsRead: true}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	h := NewEmailHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", user.ID) })
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
	user := models.User{Username: "deleted-owner"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{UserID: user.ID}
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
	r.Use(func(c *gin.Context) { c.Set("user_id", user.ID) })
	r.GET("/emails/:id", h.GetEmail)
	r.POST("/emails/:id/read", h.MarkAsRead)
	r.DELETE("/emails/:id", h.DeleteEmail)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		path := fmt.Sprintf("/emails/%d", email.ID)
		if method == http.MethodPost {
			path += "/read"
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("deleted account email accessible via %s: %d", method, w.Code)
		}
	}
}
