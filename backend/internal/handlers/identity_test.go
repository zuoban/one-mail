package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"github.com/gin-gonic/gin"
)

func TestAccountIdentityUpdateClearsCacheAtomically(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("commit_failure=%t", fail), func(t *testing.T) {
			db := setupEmailHandlerTestDB(t)
			account := models.EmailAccount{
				Email:        "owner@example.com",
				Provider:     "custom",
				Username:     "old",
				IMAPHost:     "mail.example.com",
				IMAPPort:     993,
				UIDValidity:  9,
				LastSyncTime: time.Now(),
			}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{AccountID: account.ID, UID: 1, UIDValidity: 9, MessageID: "old", BodyFetched: true, BodyText: "private body"}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			if fail {
				if err := db.Exec("CREATE TRIGGER reject_account_change BEFORE UPDATE OF identity_version ON email_accounts BEGIN SELECT RAISE(FAIL, 'commit failed'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			r := gin.New()
			r.PUT("/accounts/:id", newTestAccountHandler(t, db).UpdateAccount)
			req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/accounts/%d", account.ID), strings.NewReader(`{"username":"new"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var current models.EmailAccount
			if err := db.First(&current, account.ID).Error; err != nil {
				t.Fatal(err)
			}
			var cached []models.Email
			if err := db.Find(&cached).Error; err != nil {
				t.Fatal(err)
			}
			if fail {
				if w.Code != http.StatusInternalServerError || current.Username != "old" || current.IdentityVersion != 0 || len(cached) != 1 || cached[0].BodyText != email.BodyText {
					t.Fatalf("failed update changed identity/cache: status=%d account=%+v cache=%+v", w.Code, current, cached)
				}
			} else if w.Code != http.StatusOK || current.Username != "new" || current.IdentityVersion != 1 || current.ConfigVersion != 1 || current.UIDValidity != 0 || !current.LastSyncTime.IsZero() || len(cached) != 0 {
				t.Fatalf("identity update retained stale state: status=%d account=%+v cache=%+v", w.Code, current, cached)
			}
		})
	}
}

func TestPasswordRotationPreservesCachedBody(t *testing.T) {
	db := setupEmailHandlerTestDB(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{
		Email:           "owner@example.com",
		Provider:        "custom",
		Username:        "same",
		IMAPHost:        "mail.example.com",
		IMAPPort:        993,
		UIDValidity:     9,
		IdentityVersion: 3,
		ConfigVersion:   4,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, IdentityVersion: 3, MessageID: "same", BodyText: "cached body", BodyFetched: true}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/accounts/:id", newTestAccountHandler(t, db).UpdateAccount)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/accounts/%d", account.ID), strings.NewReader(`{"password":"rotated-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var current models.EmailAccount
	if err := db.First(&current, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	var cached models.Email
	if err := db.First(&cached, email.ID).Error; err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || current.IdentityVersion != 3 || current.ConfigVersion != 5 || current.UIDValidity != 9 || cached.BodyText != email.BodyText || !cached.BodyFetched {
		t.Fatalf("password rotation discarded cache: status=%d account=%+v email=%+v", w.Code, current, cached)
	}
}

func TestCachedBodyRejectsDifferentMailboxVersion(t *testing.T) {
	db := setupEmailHandlerTestDB(t)
	account := models.EmailAccount{IdentityVersion: 1, UIDValidity: 9}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, UID: 1, UIDValidity: 9, MessageID: "old", BodyFetched: true, BodyText: "old mailbox body"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/emails/:id", newTestEmailHandler(t, db).GetEmail)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/emails/%d", email.ID), nil))
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), email.BodyText) {
		t.Fatalf("stale body exposed: %d %s", w.Code, w.Body.String())
	}
}
