package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"one-mail/backend/internal/application"
	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func telegramRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func telegramTestRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	service, _ := newTestMailService(t, db)
	h := NewTelegramHandler(application.NewTelegramService(db, service, telegram.NewClient()))
	r := gin.New()
	r.GET("/telegram", h.GetTelegramConfig)
	r.PUT("/telegram", h.UpdateTelegramConfig)
	r.POST("/telegram/test", h.TestTelegramConnection)
	r.POST("/emails/:id/telegram", h.SendEmailToTelegram)
	return r
}

func TestTelegramConfigurationUsesInstanceSingleton(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	extra := models.TelegramConfig{ID: 2, Enabled: true, BotToken: "unrelated-token", ChatID: "unrelated-chat"}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	r := telegramTestRouter(t, db)
	w := telegramRequest(r, http.MethodGet, "/telegram", "")
	var response struct {
		Data application.TelegramConfig `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.Enabled || response.Data.BotToken != "" || response.Data.ChatID != "" {
		t.Fatalf("new instance did not receive an empty singleton: %d %s", w.Code, w.Body.String())
	}
	const bot = "instance-private-bot-token"
	w = telegramRequest(r, http.MethodPut, "/telegram", fmt.Sprintf(`{"enabled":true,"bot_token":%q,"chat_id":"instance-chat"}`, bot))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), bot) {
		t.Fatalf("config update failed or exposed the complete token: %d %s", w.Code, w.Body.String())
	}
	var stored models.TelegramConfig
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	plain, err := credentials.Decrypt(stored.BotToken)
	if err != nil || plain != bot || !credentials.IsEncrypted(stored.BotToken) || stored.ChatID != "instance-chat" || !stored.Enabled {
		t.Fatal("singleton token was not encrypted at rest")
	}
	sealed := stored.BotToken
	w = telegramRequest(r, http.MethodGet, "/telegram", "")
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.ChatID != stored.ChatID || response.Data.BotToken != application.MaskToken(bot) {
		t.Fatalf("singleton read returned wrong configuration: %s", w.Body.String())
	}
	w = telegramRequest(r, http.MethodPut, "/telegram", fmt.Sprintf(`{"enabled":false,"bot_token":%q,"chat_id":"updated-chat"}`, application.MaskToken(bot)))
	if w.Code != http.StatusOK {
		t.Fatalf("disabling singleton failed: %d %s", w.Code, w.Body.String())
	}
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BotToken != sealed || stored.ChatID != "updated-chat" || stored.Enabled {
		t.Fatal("masked token update replaced credentials or ignored disabled state")
	}
	var unchanged models.TelegramConfig
	if err := db.First(&unchanged, extra.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.BotToken != extra.BotToken || unchanged.ChatID != extra.ChatID {
		t.Fatal("singleton update modified a different row")
	}
}

func TestTelegramForwardingRejectsMissingEvictedAndRemovedAccountEmails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	active := models.EmailAccount{Email: "first@example.com"}
	second := models.EmailAccount{Email: "second@example.com"}
	deleted := models.EmailAccount{Email: "deleted@example.com"}
	for _, account := range []*models.EmailAccount{&active, &second, &deleted} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	firstEmail := models.Email{AccountID: active.ID, MessageID: "first", From: "sender@example.com", To: active.Email}
	secondEmail := models.Email{AccountID: second.ID, MessageID: "second", From: "sender@example.com", To: second.Email}
	deletedAccountEmail := models.Email{AccountID: deleted.ID, MessageID: "deleted-account", From: "sender@example.com", To: deleted.Email}
	evictedEmail := models.Email{AccountID: active.ID, MessageID: "evicted-email", From: "sender@example.com", To: active.Email}
	for _, email := range []*models.Email{&firstEmail, &secondEmail, &deletedAccountEmail, &evictedEmail} {
		if err := db.Create(email).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&evictedEmail).Error; err != nil {
		t.Fatal(err)
	}
	r := telegramTestRouter(t, db)
	for _, id := range []uint{deletedAccountEmail.ID, evictedEmail.ID, evictedEmail.ID + 100} {
		w := telegramRequest(r, http.MethodPost, fmt.Sprintf("/emails/%d/telegram", id), "")
		if w.Code != http.StatusNotFound || w.Body.String() != `{"error":"email not found"}` {
			t.Fatalf("inaccessible email returned %d: %s", w.Code, w.Body.String())
		}
	}
	for _, email := range []models.Email{firstEmail, secondEmail} {
		w := telegramRequest(r, http.MethodPost, fmt.Sprintf("/emails/%d/telegram", email.ID), "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not configured") {
			t.Fatalf("shared email did not reach singleton configuration check: %d %s", w.Code, w.Body.String())
		}
	}
	w := telegramRequest(r, http.MethodPost, "/telegram/test", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty credentials returned %d", w.Code)
	}
}

func TestTelegramEncryptedTokenWithWrongKeyIsNotExposed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	const secret = "private-telegram-token"
	sealed, err := credentials.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: sealed, ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	other := make([]byte, 32)
	other[0] = 1
	if err := credentials.Init(base64.StdEncoding.EncodeToString(other), ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	})
	r := telegramTestRouter(t, db)
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/telegram", ""},
		{http.MethodPut, "/telegram", `{"enabled":false}`},
		{http.MethodPost, "/telegram/test", `{}`},
	} {
		w := telegramRequest(r, req.method, req.path, req.body)
		if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), sealed) {
			t.Fatalf("unreadable token was exposed or accepted: %d %s", w.Code, w.Body.String())
		}
	}
	var stored models.TelegramConfig
	if err := db.First(&stored, 1).Error; err != nil || stored.BotToken != sealed {
		t.Fatalf("failed request replaced encrypted credentials: %v", err)
	}
}
