package application

import (
	"context"
	"errors"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/telegram"

	"gorm.io/gorm"
)

type NotificationItem struct {
	ID            uint      `json:"id"`
	AccountID     uint      `json:"account_id"`
	AccountEmail  string    `json:"account_email"`
	CreatedAt     time.Time `json:"created_at"`
	Attempts      uint      `json:"attempts"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Status        string    `json:"status"`
	LastError     string    `json:"last_error"`
}

type NotificationList struct {
	Data         []NotificationItem `json:"data"`
	PendingCount int64              `json:"pending_count"`
	FailedCount  int64              `json:"failed_count"`
}

func (s *TelegramService) Tasks(ctx context.Context, status string, limit int) (NotificationList, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || (status != "" && status != "pending" && status != "failed") {
		return NotificationList{}, failure(Invalid, "invalid notification query", nil)
	}
	db := s.db.WithContext(ctx)
	result := NotificationList{Data: make([]NotificationItem, 0)}
	if err := telegram.PendingTasks(db).Count(&result.PendingCount).Error; err != nil {
		return result, failure(Internal, "failed to count notification tasks", err)
	}
	if err := telegram.FailedTasks(db).Count(&result.FailedCount).Error; err != nil {
		return result, failure(Internal, "failed to count notification tasks", err)
	}
	query := db.Model(&models.NotificationTask{})
	if status == "pending" {
		query = telegram.PendingTasks(db)
	} else if status == "failed" {
		query = telegram.FailedTasks(db)
	}
	var tasks []models.NotificationTask
	if err := query.Select("id, account_id, account_email, created_at, attempts, next_attempt_at, last_error").
		Order("created_at DESC, id DESC").Limit(limit).Find(&tasks).Error; err != nil {
		return result, failure(Internal, "failed to load notification tasks", err)
	}
	for _, task := range tasks {
		status := "pending"
		if telegram.TaskFailed(&task) {
			status = "failed"
		}
		result.Data = append(result.Data, NotificationItem{
			ID: task.ID, AccountID: task.AccountID, AccountEmail: task.AccountEmail,
			CreatedAt: task.CreatedAt, Attempts: task.Attempts, NextAttemptAt: task.NextAttemptAt,
			Status: status, LastError: task.LastError,
		})
	}
	return result, nil
}

func (s *TelegramService) changeFailedTask(ctx context.Context, id uint, remove bool) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.NotificationTask
		if err := tx.Select("id, attempts, last_error").First(&task, id).Error; err != nil {
			return err
		}
		if !telegram.TaskFailed(&task) {
			return failure(Conflict, "only failed notification tasks can be changed", nil)
		}
		if remove {
			return tx.Delete(&task).Error
		}
		return tx.Model(&task).Updates(map[string]interface{}{
			"attempts": 0, "last_error": "", "next_attempt_at": time.Now(),
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(NotFound, "notification task not found", err)
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return err
		}
		return failure(Internal, "failed to update notification task", err)
	}
	return nil
}

func (s *TelegramService) RetryTask(ctx context.Context, id uint) error {
	return s.changeFailedTask(ctx, id, false)
}

func (s *TelegramService) DeleteTask(ctx context.Context, id uint) error {
	return s.changeFailedTask(ctx, id, true)
}
