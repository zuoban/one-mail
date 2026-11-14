package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (f telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestConfigurationCacheAndDeliveryAreScopedByOwner(t *testing.T) {
	oldDB := database.DB
	configCacheMu.Lock()
	oldCache := cachedConfigs
	cachedConfigs = make(map[uint]*models.TelegramConfig)
	configCacheMu.Unlock()
	t.Cleanup(func() {
		database.DB = oldDB
		configCacheMu.Lock()
		cachedConfigs = oldCache
		configCacheMu.Unlock()
	})
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "telegram.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.TelegramConfig{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	configs := []models.TelegramConfig{
		{UserID: 0, Enabled: true, BotToken: "unassigned-global-token", ChatID: "legacy-chat"},
		{UserID: 1, Enabled: true, BotToken: "alice-token", ChatID: "alice-chat"},
		{UserID: 2, Enabled: true, BotToken: "bob-token", ChatID: "bob-chat"},
	}
	if err := db.Create(&configs).Error; err != nil {
		t.Fatal(err)
	}
	if getConfig(0) != nil || getConfig(99) != nil {
		t.Fatal("unassigned configuration must never be used as an owner fallback")
	}
	first, second := getConfig(1), getConfig(2)
	if first == nil || second == nil || first.ChatID != "alice-chat" || second.ChatID != "bob-chat" {
		t.Fatal("owners did not load isolated configuration")
	}
	type delivery struct{ token, chat string }
	var deliveries []delivery
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery{req.URL.Path, payload.ChatID})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}}
	email := &models.Email{Subject: "private mail", From: "sender@example.com"}
	if err := client.SendNewEmail(1, email, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := client.SendNewEmail(2, email, "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TelegramConfig{}).Where("user_id = ?", 1).
		Updates(map[string]interface{}{"bot_token": "alice-new-token", "chat_id": "alice-new-chat"}).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateConfigCache(1)
	if getConfig(2) != second {
		t.Fatal("invalidating one owner cleared another owner's cached config")
	}
	if err := client.SendNewEmail(1, email, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := client.SendNewEmail(99, email, "missing@example.com"); err != nil {
		t.Fatal(err)
	}
	want := []delivery{{"/botalice-token/sendMessage", "alice-chat"}, {"/botbob-token/sendMessage", "bob-chat"}, {"/botalice-new-token/sendMessage", "alice-new-chat"}}
	if len(deliveries) != len(want) {
		t.Fatalf("unexpected delivery count: %d", len(deliveries))
	}
	for i := range want {
		if deliveries[i] != want[i] {
			t.Fatalf("delivery %d used another owner's config: %+v", i, deliveries[i])
		}
	}
}

func TestTelegramTransportErrorsDoNotExposeBotToken(t *testing.T) {
	const secret = "private-bot-token"
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request failed for %s", req.URL.String())
	})}}
	for _, err := range []error{
		client.sendMessage(secret, "chat", "message"),
		client.TestConnection(secret, "chat"),
		client.sendMessage("bad\ntoken", "chat", "message"),
	} {
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "api.telegram.org") {
			t.Fatalf("transport failure was not sanitized: %v", err)
		}
	}
}
