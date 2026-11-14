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

func TestConfigurationCacheAndDeliveryUseSingleton(t *testing.T) {
	oldDB := database.DB
	configCacheMu.Lock()
	oldCache := cachedConfig
	cachedConfig = nil
	configCacheMu.Unlock()
	t.Cleanup(func() {
		database.DB = oldDB
		configCacheMu.Lock()
		cachedConfig = oldCache
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
	extra := models.TelegramConfig{ID: 2, Enabled: true, BotToken: "unrelated-token", ChatID: "unrelated-chat"}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
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
	if getConfig() != nil || client.IsEnabled() {
		t.Fatal("missing singleton must not load another configuration")
	}
	if err := client.SendNewEmail(email, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 0 {
		t.Fatal("missing singleton sent a notification")
	}
	cfg := models.TelegramConfig{ID: 1, Enabled: true, BotToken: "instance-token", ChatID: "instance-chat"}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	first := getConfig()
	if first == nil || first.ID != 1 || !client.IsEnabled() {
		t.Fatal("complete singleton was not loaded")
	}
	if err := client.SendNewEmail(email, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TelegramConfig{}).Where("id = ?", 1).
		Updates(map[string]interface{}{"bot_token": "updated-token", "chat_id": "updated-chat"}).Error; err != nil {
		t.Fatal(err)
	}
	if getConfig() != first {
		t.Fatal("cached configuration changed without invalidation")
	}
	InvalidateConfigCache()
	if refreshed := getConfig(); refreshed == nil || refreshed == first || refreshed.BotToken != "updated-token" || refreshed.ChatID != "updated-chat" {
		t.Fatal("invalidation did not reload the singleton")
	}
	if err := client.SendNewEmail(email, "second@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TelegramConfig{}).Where("id = ?", 1).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateConfigCache()
	if client.IsEnabled() {
		t.Fatal("disabled configuration remained enabled")
	}
	if err := client.SendNewEmail(email, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	InvalidateConfigCache()
	if getConfig() != nil {
		t.Fatal("deleting singleton loaded another row")
	}
	if err := client.SendNewEmail(email, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	want := []delivery{{"/botinstance-token/sendMessage", "instance-chat"}, {"/botupdated-token/sendMessage", "updated-chat"}}
	if len(deliveries) != len(want) {
		t.Fatalf("unexpected delivery count: %d", len(deliveries))
	}
	for i := range want {
		if deliveries[i] != want[i] {
			t.Fatalf("delivery %d used the wrong configuration: %+v", i, deliveries[i])
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
