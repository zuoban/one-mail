package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"one-mail/backend/internal/middleware"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"
	"one-mail/backend/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func createTelegramTestUser(t *testing.T, db *gorm.DB, name string) (models.User, string) {
	t.Helper()
	user := models.User{Username: name, Password: "hash", SessionVersion: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { telegram.InvalidateConfigCache(user.ID) })
	token, err := utils.GenerateToken(user.ID, user.Username, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	return user, token
}

func telegramTestRouter() *gin.Engine {
	r := gin.New()
	r.Use(middleware.AuthMiddleware())
	r.GET("/telegram", GetTelegramConfig)
	r.PUT("/telegram", UpdateTelegramConfig)
	r.POST("/telegram/test", TestTelegramConnection)
	r.POST("/emails/:id/telegram", SendEmailToTelegram)
	return r
}

func TestTelegramConfigurationIsOwnedByAuthenticatedUser(t *testing.T) {
	db := setupSecurityHandlerDB(t)
	first, firstToken := createTelegramTestUser(t, db, "alice")
	second, secondToken := createTelegramTestUser(t, db, "bob")
	legacy := models.TelegramConfig{Enabled: true, BotToken: "legacy-private-token", ChatID: "legacy-chat"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	r := telegramTestRouter()
	for _, token := range []string{firstToken, secondToken} {
		w := securityRequest(t, r, http.MethodGet, "/telegram", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("config fetch returned %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Data TelegramConfigResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Enabled || response.Data.BotToken != "" || response.Data.ChatID != "" {
			t.Fatal("unassigned legacy configuration leaked to an owner")
		}
	}
	for _, tc := range []struct {
		token string
		owner uint
		other uint
		bot   string
		chat  string
	}{
		{firstToken, first.ID, second.ID, "alice-private-bot-token", "alice-chat"},
		{secondToken, second.ID, first.ID, "bob-private-bot-token", "bob-chat"},
	} {
		body := fmt.Sprintf(`{"enabled":true,"bot_token":%q,"chat_id":%q,"user_id":%d}`, tc.bot, tc.chat, tc.other)
		w := securityRequest(t, r, http.MethodPut, "/telegram", tc.token, body)
		if w.Code != http.StatusOK {
			t.Fatalf("config update returned %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), tc.bot) {
			t.Fatal("response exposed the complete bot token")
		}
	}
	for _, tc := range []struct {
		token string
		owner uint
		bot   string
		chat  string
	}{
		{firstToken, first.ID, "alice-private-bot-token", "alice-chat"},
		{secondToken, second.ID, "bob-private-bot-token", "bob-chat"},
	} {
		var stored models.TelegramConfig
		if err := db.Where("user_id = ?", tc.owner).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.BotToken != tc.bot || stored.ChatID != tc.chat || !stored.Enabled {
			t.Fatal("updating another user changed this owner's configuration")
		}
		w := securityRequest(t, r, http.MethodGet, "/telegram?user_id=0", tc.token, "")
		var response struct {
			Data TelegramConfigResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || response.Data.ChatID != tc.chat || response.Data.BotToken != maskToken(tc.bot) {
			t.Fatalf("owner read returned wrong configuration: %s", w.Body.String())
		}
	}
	var unchanged models.TelegramConfig
	if err := db.First(&unchanged, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.BotToken != legacy.BotToken || unchanged.ChatID != legacy.ChatID {
		t.Fatal("owner configuration update modified legacy global configuration")
	}
}

func TestTelegramForwardingRejectsForeignAndDeletedEmails(t *testing.T) {
	db := setupSecurityHandlerDB(t)
	owner, token := createTelegramTestUser(t, db, "owner")
	other, _ := createTelegramTestUser(t, db, "other")
	active := models.EmailAccount{UserID: owner.ID, Email: "owner@example.com"}
	foreign := models.EmailAccount{UserID: other.ID, Email: "other@example.com"}
	deleted := models.EmailAccount{UserID: owner.ID, Email: "deleted@example.com"}
	for _, account := range []*models.EmailAccount{&active, &foreign, &deleted} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	ownEmail := models.Email{AccountID: active.ID, MessageID: "own", From: "sender@example.com", To: active.Email}
	foreignEmail := models.Email{AccountID: foreign.ID, MessageID: "foreign", From: "sender@example.com", To: foreign.Email}
	foreignConfig := models.TelegramConfig{UserID: other.ID, Enabled: true, BotToken: "other-private-token", ChatID: "other-chat"}
	if err := db.Create(&foreignConfig).Error; err != nil {
		t.Fatal(err)
	}
	deletedAccountEmail := models.Email{AccountID: deleted.ID, MessageID: "deleted-account", From: "sender@example.com", To: deleted.Email}
	deletedEmail := models.Email{AccountID: active.ID, MessageID: "deleted-email", From: "sender@example.com", To: active.Email}
	for _, email := range []*models.Email{&ownEmail, &foreignEmail, &deletedAccountEmail, &deletedEmail} {
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
	for _, email := range []models.Email{foreignEmail, deletedAccountEmail, deletedEmail} {
		w := securityRequest(t, r, http.MethodPost, fmt.Sprintf("/emails/%d/telegram", email.ID), token, "")
		if w.Code != http.StatusNotFound || w.Body.String() != `{"error":"email not found"}` {
			t.Fatalf("inaccessible email returned %d: %s", w.Code, w.Body.String())
		}
	}
	w := securityRequest(t, r, http.MethodPost, fmt.Sprintf("/emails/%d/telegram", ownEmail.ID), token, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not configured") {
		t.Fatalf("owned email did not reach owner configuration check: %d %s", w.Code, w.Body.String())
	}
	w = securityRequest(t, r, http.MethodPost, "/telegram/test", token, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty owner credentials returned %d", w.Code)
	}
}
