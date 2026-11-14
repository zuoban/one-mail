package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

// Dispatcher delivers committed snapshots. Delivery is at least once: a crash
// after Telegram accepts a request but before deletion can repeat that message.
// There is one dispatcher per single-user instance; no database lock is held
// during network I/O. Cancellation leaves unacknowledged tasks available.
type Dispatcher struct {
	db     *gorm.DB
	client *Client
}

func NewDispatcher(db *gorm.DB, client *Client) *Dispatcher {
	return &Dispatcher{db: db, client: client}
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := d.deliverBatch(ctx, time.Now(), 32); err != nil && ctx.Err() == nil {
			// Database and transport errors must not expose credentials or payloads.
			log.Print("event=notification_poll_failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) deliverBatch(ctx context.Context, now time.Time, limit int) error {
	db := d.db.WithContext(ctx)
	var cfg models.TelegramConfig
	if err := db.First(&cfg, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// Disabling notifications pauses outstanding deliveries; re-enabling resumes
	// them. New tasks are recorded only while notification settings are enabled.
	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		return nil
	}
	token, err := credentials.Decrypt(cfg.BotToken)
	if err != nil {
		return errors.New("unable to unlock notification configuration")
	}
	var tasks []models.NotificationTask
	if err := db.Where("next_attempt_at <= ?", now).Order("next_attempt_at, id").Limit(limit).Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		var account models.EmailAccount
		err := db.Select("id, identity_version").First(&account, task.AccountID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && account.IdentityVersion != task.IdentityVersion) {
			if err := db.Delete(&task).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var email models.Email
		failure := "invalid notification snapshot"
		if err = json.Unmarshal([]byte(task.EmailJSON), &email); err == nil {
			err = d.client.SendNewEmailWithConfig(ctx, &email, task.AccountEmail, token, cfg.ChatID)
			failure = "telegram delivery failed"
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			attempts := task.Attempts + 1
			if err := db.Model(&task).Updates(map[string]interface{}{
				"attempts": attempts, "next_attempt_at": now.Add(retryDelay(attempts)), "last_error": failure,
			}).Error; err != nil {
				return err
			}
			log.Printf("event=notification_retry task_id=%d account_id=%d attempts=%d", task.ID, task.AccountID, attempts)
			continue
		}
		if err := db.Delete(&task).Error; err != nil {
			return err
		}
		log.Printf("event=notification_delivered task_id=%d account_id=%d attempts=%d", task.ID, task.AccountID, task.Attempts+1)
	}
	return nil
}

func retryDelay(attempts uint) time.Duration {
	delay := 10 * time.Second
	for attempt := uint(1); attempt < attempts && delay < time.Hour; attempt++ {
		delay *= 2
	}
	return min(delay, time.Hour)
}
