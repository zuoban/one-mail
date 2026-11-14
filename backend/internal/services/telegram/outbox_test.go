package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
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

func outboxDatabase(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.EmailAccount{}, &models.TelegramConfig{}, &models.NotificationTask{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func pendingNotification(t *testing.T, db *gorm.DB, now time.Time) models.NotificationTask {
	t.Helper()
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := credentials.Encrypt("private-outbox-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: sealed, ChatID: "instance-chat"}).Error; err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{Email: "mail@example.com", IdentityVersion: 3}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(models.Email{AccountID: account.ID, Subject: "private-subject", From: "sender@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	task := models.NotificationTask{AccountID: account.ID, IdentityVersion: account.IdentityVersion, MessageKey: "one-message", AccountEmail: account.Email, EmailJSON: string(payload), NextAttemptAt: now}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}

func TestOutboxRetriesCommittedSnapshotAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	db := outboxDatabase(t, path)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	task := pendingNotification(t, db, now)
	var output bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(oldOutput) })
	requests := 0
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return nil, errors.New("private-outbox-token connection reset")
		}
		var payload struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ChatID != "instance-chat" || !strings.Contains(payload.Text, "private\\-subject") {
			t.Fatalf("persisted snapshot changed: %+v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}}
	if err := NewDispatcher(db, client).deliverBatch(context.Background(), now, 32); err != nil {
		t.Fatal(err)
	}
	var retry models.NotificationTask
	if err := db.First(&retry, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retry.Attempts != 1 || !retry.NextAttemptAt.After(now) || strings.Contains(retry.LastError, "private") {
		t.Fatalf("failed delivery was not durably rescheduled: %+v", retry)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := outboxDatabase(t, path)
	if err := NewDispatcher(reopened, client).deliverBatch(context.Background(), retry.NextAttemptAt, 32); err != nil {
		t.Fatal(err)
	}
	if err := reopened.First(&retry, task.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("acknowledged task remains: %v", err)
	}
	if requests != 2 {
		t.Fatalf("unexpected delivery requests: %d", requests)
	}
	for _, secret := range []string{"private-subject", "private-outbox-token", "sender@example.com"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("notification log disclosed %q", secret)
		}
	}
}

func TestOutboxPauseAndIdentityInvalidation(t *testing.T) {
	db := outboxDatabase(t, filepath.Join(t.TempDir(), "outbox.db"))
	now := time.Now()
	task := pendingNotification(t, db, now)
	if err := db.Model(&models.TelegramConfig{}).Where("id = 1").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("paused or invalidated notification reached transport")
		return nil, nil
	})}}
	dispatcher := NewDispatcher(db, client)
	if err := dispatcher.deliverBatch(context.Background(), now, 32); err != nil {
		t.Fatal(err)
	}
	var retained models.NotificationTask
	if err := db.First(&retained, task.ID).Error; err != nil {
		t.Fatalf("paused task lost: %v", err)
	}
	if err := db.Model(&models.TelegramConfig{}).Where("id = 1").Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.EmailAccount{}).Where("id = ?", task.AccountID).Update("identity_version", 4).Error; err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.deliverBatch(context.Background(), now, 32); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&retained, task.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("old mailbox task was not discarded: %v", err)
	}
}

func TestOutboxCancellationRetainsUnacknowledgedTask(t *testing.T) {
	db := outboxDatabase(t, filepath.Join(t.TempDir(), "outbox.db"))
	now := time.Now()
	task := pendingNotification(t, db, now)
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		return nil, req.Context().Err()
	})}}
	if err := NewDispatcher(db, client).deliverBatch(ctx, now, 32); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delivery returned %v", err)
	}
	var retained models.NotificationTask
	if err := db.First(&retained, task.ID).Error; err != nil || retained.Attempts != 0 {
		t.Fatalf("shutdown acknowledged or retried task: %+v %v", retained, err)
	}
}
