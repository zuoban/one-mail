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
	account := models.EmailAccount{Password: sealed}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "cached", BodyText: "private cached body", BodyFetched: true}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	h := NewAccountHandler()
	r := gin.New()
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
	if err := db.Unscoped().Model(&models.Email{}).Where("account_id = ?", account.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("deleted account left physical cached messages")
	}
}

func TestBodyFetchFailureDoesNotReturnEmptySuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{IMAPHost: "127.0.0.1", IMAPPort: 1, Username: "owner", Password: "temporary", UIDValidity: 11}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "body-failure", UID: 1, UIDValidity: 11}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
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

func TestMissingAndDeletedAccountsRemainUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "deleted@example.com", Username: "remote-login", Password: "remote-password"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&account).Error; err != nil {
		t.Fatal(err)
	}
	h := NewAccountHandler()
	emails := NewEmailHandler()
	r := gin.New()
	r.GET("/accounts", h.ListAccounts)
	r.PUT("/accounts/:id", h.UpdateAccount)
	r.DELETE("/accounts/:id", h.DeleteAccount)
	r.POST("/accounts/:id/test", h.TestAccount)
	r.POST("/accounts/:id/refresh", emails.RefreshAccountEmails)
	r.POST("/emails/refresh", emails.RefreshEmails)
	for _, id := range []uint{account.ID, account.ID + 100} {
		for _, operation := range []struct{ method, suffix string }{
			{http.MethodPut, ""},
			{http.MethodDelete, ""},
			{http.MethodPost, "/test"},
			{http.MethodPost, "/refresh"},
		} {
			req := httptest.NewRequest(operation.method, fmt.Sprintf("/accounts/%d%s", id, operation.suffix), strings.NewReader(`{"username":"replacement"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("unavailable mailbox %d accessible via %s %s: %d", id, operation.method, operation.suffix, w.Code)
			}
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/emails/refresh?account_id=%d", id), nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("refresh included unavailable mailbox %d: %d", id, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/accounts", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"data":[]}` {
		t.Fatalf("deleted mailbox appeared in account list: %d %s", w.Code, w.Body.String())
	}
	var stored models.EmailAccount
	if err := db.Unscoped().First(&stored, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.DeletedAt.Valid || stored.Username != account.Username || stored.Password != account.Password {
		t.Fatal("unavailable mailbox credentials or deletion state were modified")
	}
}

func TestAccountDeleteRollsBackCacheCleanupOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{Email: "rollback@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "cached", BodyFetched: true, BodyText: "private cached body"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_account_delete BEFORE UPDATE OF deleted_at ON email_accounts BEGIN SELECT RAISE(FAIL, 'account delete failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.DELETE("/accounts/:id", NewAccountHandler().DeleteAccount)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/accounts/%d", account.ID), nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failed deletion returned %d: %s", w.Code, w.Body.String())
	}
	var stored models.Email
	if err := db.First(&stored, email.ID).Error; err != nil || stored.BodyText != email.BodyText || !stored.BodyFetched {
		t.Fatalf("failed account deletion destroyed cached body: %+v %v", stored, err)
	}
	var active models.EmailAccount
	if err := db.First(&active, account.ID).Error; err != nil {
		t.Fatalf("failed deletion removed account: %v", err)
	}
}
