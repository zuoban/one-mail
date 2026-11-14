package telegram

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/models"
)

func TestPermanentAndExhaustedTasksStopAutomaticDelivery(t *testing.T) {
	for _, code := range []int{400, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			db := outboxDatabase(t, filepath.Join(t.TempDir(), "outbox.db"))
			now := time.Now()
			task := pendingNotification(t, db, now)
			if code == 500 {
				if err := db.Model(&task).Update("attempts", MaxDeliveryAttempts-1).Error; err != nil {
					t.Fatal(err)
				}
			}
			requests := 0
			client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode: code, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"ok":false}`)),
				}, nil
			})}}
			dispatcher := NewDispatcher(db, client)
			if err := dispatcher.deliverBatch(context.Background(), now, 32); err != nil {
				t.Fatal(err)
			}
			var saved models.NotificationTask
			if err := db.First(&saved, task.ID).Error; err != nil || !TaskFailed(&saved) {
				t.Fatalf("task did not reach a durable terminal state: %+v %v", saved, err)
			}
			if err := dispatcher.deliverBatch(context.Background(), now.Add(2*time.Hour), 32); err != nil || requests != 1 {
				t.Fatalf("terminal task was sent again: requests=%d error=%v", requests, err)
			}
		})
	}
}

func TestRateLimitDurablyDefersTheEntireBotQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	db := outboxDatabase(t, path)
	now := time.Now()
	task := pendingNotification(t, db, now)
	second := task
	second.ID = 0
	second.MessageKey = "second-message"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: 429, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`)),
		}, nil
	})}}
	if err := NewDispatcher(db, client).deliverBatch(context.Background(), now, 32); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("rate limiting did not stop the batch: %d", requests)
	}
	var tasks []models.NotificationTask
	if err := db.Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	for _, saved := range tasks {
		if saved.NextAttemptAt.Before(now.Add(time.Minute)) || TaskFailed(&saved) {
			t.Fatalf("rate limit was not persisted for every pending task: %+v", saved)
		}
	}
	arrival := task
	arrival.ID = 0
	arrival.MessageKey = "arrived-during-cooldown"
	if err := db.Create(&arrival).Error; err != nil {
		t.Fatal(err)
	}
	reopened := outboxDatabase(t, path)
	if err := NewDispatcher(reopened, client).deliverBatch(context.Background(), now.Add(30*time.Second), 32); err != nil || requests != 1 {
		t.Fatalf("restart forgot the cooldown: requests=%d error=%v", requests, err)
	}
	client.httpClient.Transport = telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})
	if err := NewDispatcher(reopened, client).deliverBatch(context.Background(), now.Add(2*time.Minute), 32); err != nil || requests != 4 {
		t.Fatalf("queue did not resume after cooldown: requests=%d error=%v", requests, err)
	}
}

func TestDisablingNotificationsStopsTheRemainingBatch(t *testing.T) {
	db := outboxDatabase(t, filepath.Join(t.TempDir(), "outbox.db"))
	now := time.Now()
	first := pendingNotification(t, db, now)
	second := first
	second.ID = 0
	second.MessageKey = "second-message"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if err := db.Model(&models.TelegramConfig{}).Where("id = 1").Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}}
	if err := NewDispatcher(db, client).deliverBatch(context.Background(), now, 32); err != nil || requests != 1 {
		t.Fatalf("disabled configuration did not stop delivery: requests=%d error=%v", requests, err)
	}
	if err := db.First(&second, second.ID).Error; err != nil {
		t.Fatalf("paused task was lost: %v", err)
	}
}

func TestMalformedNotificationSnapshotBecomesTerminal(t *testing.T) {
	db := outboxDatabase(t, filepath.Join(t.TempDir(), "outbox.db"))
	now := time.Now()
	task := pendingNotification(t, db, now)
	if err := db.Model(&task).Update("email_json", "{invalid").Error; err != nil {
		t.Fatal(err)
	}
	if err := NewDispatcher(db, nil).deliverBatch(context.Background(), now, 32); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, task.ID).Error; err != nil || !TaskFailed(&task) {
		t.Fatalf("invalid snapshot remained pending: %+v %v", task, err)
	}
}
