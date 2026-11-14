package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"

	"github.com/gin-gonic/gin"
)

func telegramRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func telegramTestRouter() *gin.Engine {
	r := gin.New()
	r.GET("/telegram", GetTelegramConfig)
	r.PUT("/telegram", UpdateTelegramConfig)
	r.POST("/telegram/test", TestTelegramConnection)
	r.POST("/emails/:id/telegram", SendEmailToTelegram)
	return r
}

func TestTelegramConfigurationUsesInstanceSingleton(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEmailHandlerTestDB(t)
	telegram.InvalidateConfigCache()
	t.Cleanup(telegram.InvalidateConfigCache)
	extra := models.TelegramConfig{ID: 2, Enabled: true, BotToken: "unrelated-token", ChatID: "unrelated-chat"}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	r := telegramTestRouter()
	w := telegramRequest(r, http.MethodGet, "/telegram", "")
	var response struct {
		Data TelegramConfigResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.Enabled || response.Data.BotToken != "" || response.Data.ChatID != "" {
		t.Fatalf("new instance did not receive an empty singleton: %d %s", w.Code, w.Body.String())
	}
	if telegram.GetClient().IsEnabled() {
		t.Fatal("empty singleton used another configuration")
	}
	const bot = "instance-private-bot-token"
	w = telegramRequest(r, http.MethodPut, "/telegram", fmt.Sprintf(`{"enabled":true,"bot_token":%q,"chat_id":"instance-chat"}`, bot))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), bot) {
		t.Fatalf("config update failed or exposed the complete token: %d %s", w.Code, w.Body.String())
	}
	if !telegram.GetClient().IsEnabled() {
		t.Fatal("configuration update did not invalidate the cached singleton")
	}
	var stored models.TelegramConfig
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BotToken != bot || stored.ChatID != "instance-chat" || !stored.Enabled {
		t.Fatalf("singleton was not saved: %+v", stored)
	}
	w = telegramRequest(r, http.MethodGet, "/telegram", "")
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.Data.ChatID != stored.ChatID || response.Data.BotToken != maskToken(bot) {
		t.Fatalf("singleton read returned wrong configuration: %s", w.Body.String())
	}
	w = telegramRequest(r, http.MethodPut, "/telegram", fmt.Sprintf(`{"enabled":false,"bot_token":%q,"chat_id":"updated-chat"}`, maskToken(bot)))
	if w.Code != http.StatusOK || telegram.GetClient().IsEnabled() {
		t.Fatalf("disabling singleton did not invalidate cache: %d %s", w.Code, w.Body.String())
	}
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BotToken != bot || stored.ChatID != "updated-chat" || stored.Enabled {
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

func TestTelegramForwardingRejectsMissingAndDeletedEmails(t *testing.T) {
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
	deletedEmail := models.Email{AccountID: active.ID, MessageID: "deleted-email", From: "sender@example.com", To: active.Email}
	for _, email := range []*models.Email{&firstEmail, &secondEmail, &deletedAccountEmail, &deletedEmail} {
		if err := db.Create(email).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&deletedEmail).Error; err != nil {
		t.Fatal(err)
	}
	r := telegramTestRouter()
	for _, id := range []uint{deletedAccountEmail.ID, deletedEmail.ID, deletedEmail.ID + 100} {
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
