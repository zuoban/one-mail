package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"one-mail/backend/internal/models"

	"github.com/gin-gonic/gin"
)

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
	// Older versions could leave cached messages belonging to a removed account.
	if err := db.Delete(&account).Error; err != nil {
		t.Fatal(err)
	}
	h := NewEmailHandler()
	r := gin.New()
	r.GET("/emails/:id", h.GetEmail)
	r.GET("/emails", h.ListEmails)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails/%d", email.ID), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted account message accessible: %d %s", w.Code, w.Body.String())
	}
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
}

func TestMissingAndEvictedEmailsRemainUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "mailbox@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "evicted-message", BodyText: "cached body"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&email).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/emails/:id", NewEmailHandler().GetEmail)
	for _, id := range []uint{email.ID, email.ID + 100} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails/%d", id), nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("unavailable message %d accessible: %d", id, w.Code)
		}
	}
	var count int64
	if err := db.Unscoped().Model(&models.Email{}).Where("id = ?", email.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("evicted email retained a physical record")
	}
}

func TestCachedBodiesDoNotRequireIMAP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "cached@example.com", IMAPHost: "127.0.0.1", IMAPPort: 1, UIDValidity: 11}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/emails/:id", NewEmailHandler().GetEmail)
	for _, body := range []string{"cached content", ""} {
		email := models.Email{AccountID: account.ID, MessageID: "cached", UID: uint(len(body) + 1), UIDValidity: 11, BodyFetched: true, BodyText: body}
		if err := db.Create(&email).Error; err != nil {
			t.Fatal(err)
		}
		for range 2 {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails/%d", email.ID), nil))
			var response struct {
				Data models.Email `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || response.Data.BodyText != body {
				t.Fatalf("cached body was not reused: %d %s", w.Code, w.Body.String())
			}
		}
	}
}
