package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (f telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTelegramTransportErrorsDoNotExposeBotToken(t *testing.T) {
	const secret = "private-bot-token"
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("request failed for %s", req.URL.String())
	})}}
	for _, err := range []error{
		client.sendMessage(context.Background(), secret, "chat", "message"),
		client.TestConnection(context.Background(), secret, "chat"),
		client.sendMessage(context.Background(), "bad\ntoken", "chat", "message"),
	} {
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "api.telegram.org") {
			t.Fatalf("transport failure was not sanitized: %v", err)
		}
	}
}

func TestEncryptedConfigurationRejectsWrongKeyWithoutDelivery(t *testing.T) {
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := credentials.Encrypt("private-token")
	if err != nil {
		t.Fatal(err)
	}
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
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: sealed, ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), "") })
	other := make([]byte, 32)
	other[0] = 1
	if err := credentials.Init(base64.StdEncoding.EncodeToString(other), ""); err != nil {
		t.Fatal(err)
	}
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("unreadable stored token reached HTTP transport")
		return nil, nil
	})}}
	if err := NewDispatcher(db, client).deliverBatch(context.Background(), time.Now(), 1); err == nil {
		t.Fatal("unreadable stored token was accepted")
	}
}
