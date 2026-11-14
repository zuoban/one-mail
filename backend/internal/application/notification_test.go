package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"
)

func TestNotificationManagementOnlyChangesFailedTasksAndHidesSnapshots(t *testing.T) {
	db, pool := applicationTestDB(t)
	service := NewTelegramService(db, NewMailService(db, pool, false), &recordingTelegram{})
	tasks := []models.NotificationTask{
		{MessageKey: "pending", AccountEmail: "owner@example.com", EmailJSON: "private-subject", NextAttemptAt: time.Now()},
		{MessageKey: "exhausted", Attempts: telegram.MaxDeliveryAttempts, EmailJSON: "private-subject", NextAttemptAt: time.Now()},
		{MessageKey: "permanent", Attempts: 1, LastError: "permanent: telegram message rejected", EmailJSON: "private-subject", NextAttemptAt: time.Now()},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.Tasks(context.Background(), "failed", 1)
	if err != nil || len(result.Data) != 1 || result.PendingCount != 1 || result.FailedCount != 2 || result.Data[0].Status != "failed" {
		t.Fatalf("task listing/filter/count mismatch: %+v %v", result, err)
	}
	payload, err := json.Marshal(result)
	if err != nil || strings.Contains(string(payload), "private-subject") || strings.Contains(string(payload), "email_json") {
		t.Fatalf("task management exposed the mail snapshot: %s %v", payload, err)
	}
	for _, remove := range []bool{false, true} {
		if err := service.changeFailedTask(context.Background(), tasks[0].ID, remove); err == nil {
			t.Fatal("in-flight or pending delivery was changed")
		}
	}
	if err := service.RetryTask(context.Background(), tasks[1].ID); err != nil {
		t.Fatal(err)
	}
	var retried models.NotificationTask
	if err := db.First(&retried, tasks[1].ID).Error; err != nil || telegram.TaskFailed(&retried) || retried.Attempts != 0 || retried.LastError != "" {
		t.Fatalf("failed task did not re-enter the queue: %+v %v", retried, err)
	}
	if err := service.DeleteTask(context.Background(), tasks[2].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Tasks(context.Background(), "unknown", 50); err == nil {
		t.Fatal("invalid task filter was accepted")
	}
	if _, err := service.Tasks(context.Background(), "", 101); err == nil {
		t.Fatal("unbounded task list was accepted")
	}
	if err := service.RetryTask(context.Background(), 999); err == nil {
		t.Fatal("missing task was retried")
	}
}
