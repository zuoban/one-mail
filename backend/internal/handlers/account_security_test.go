package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"github.com/gin-gonic/gin"
)

func TestAccountUpdateEncryptsPasswordAndDeleteCleansMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := credentials.Encrypt("original")
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "mail-owner"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{UserID: user.ID, Password: sealed}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "cached"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	h := NewAccountHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", user.ID) })
	r.PUT("/accounts/:id", h.UpdateAccount)
	r.DELETE("/accounts/:id", h.DeleteAccount)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/accounts/%d", account.ID), strings.NewReader(`{"password":"updated-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update failed: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "updated-secret") || strings.Contains(w.Body.String(), "enc:v1:") {
		t.Fatal("account response exposes credentials")
	}
	var stored models.EmailAccount
	if err := db.First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !credentials.IsEncrypted(stored.Password) {
		t.Fatal("updated password stored in plaintext")
	}
	if plain, err := credentials.Decrypt(stored.Password); err != nil || plain != "updated-secret" {
		t.Fatal("updated password cannot be recovered")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/accounts/%d", account.ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %s", w.Body.String())
	}
	var count int64
	if err := db.Model(&models.Email{}).Where("account_id = ?", account.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("deleted account left accessible messages")
	}
}

func TestBodyFetchFailureDoesNotReturnEmptySuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{UserID: 1, IMAPHost: "127.0.0.1", IMAPPort: 1, Username: "owner", Password: "temporary", UIDValidity: 11}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "body-failure", UID: 1, UIDValidity: 11}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", uint(1)) })
	r.GET("/emails/:id", NewEmailHandler().GetEmail)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails/%d", email.ID), nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("body failure returned success: %d", w.Code)
	}
	var stored models.Email
	if err := db.First(&stored, email.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BodyFetched {
		t.Fatal("failed body fetch was cached as empty")
	}
}
