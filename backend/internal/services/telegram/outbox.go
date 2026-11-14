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
	"gorm.io/gorm/clause"
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
	var deliveryState models.NotificationDeliveryState
	if err := db.First(&deliveryState, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if now.Before(deliveryState.NotBefore) {
		return nil
	}
	token, err := credentials.Decrypt(cfg.BotToken)
	if err != nil {
		return errors.New("unable to unlock notification configuration")
	}
	var tasks []models.NotificationTask
	if err := PendingTasks(db).Where("next_attempt_at <= ?", now).Order("next_attempt_at, id").Limit(limit).Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		var currentConfig models.TelegramConfig
		if err := db.First(&currentConfig, 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !currentConfig.Enabled || currentConfig.BotToken != cfg.BotToken || currentConfig.ChatID != cfg.ChatID {
			return nil
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
		failure := permanentFailurePrefix + "invalid notification snapshot"
		if err = json.Unmarshal([]byte(task.EmailJSON), &email); err == nil {
			err = d.client.SendNewEmailWithConfig(ctx, &email, task.AccountEmail, token, cfg.ChatID)
			failure = "telegram delivery failed"
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			attempts := task.Attempts + 1
			delay := retryDelay(attempts)
			var deliveryErr *DeliveryError
			limited := errors.As(err, &deliveryErr) && deliveryErr.Code == 429
			if deliveryErr != nil {
				failure = deliveryErr.Reason
				if deliveryErr.Permanent {
					failure = permanentFailurePrefix + failure
				}
				delay = max(delay, deliveryErr.RetryAfter)
			}
			completedAt := time.Now()
			if completedAt.Before(now) {
				completedAt = now
			}
			nextAttempt := completedAt.Add(delay)
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&task).Updates(map[string]interface{}{
					"attempts": attempts, "next_attempt_at": nextAttempt, "last_error": failure,
				}).Error; err != nil {
					return err
				}
				if limited {
					if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
						Create(&models.NotificationDeliveryState{ID: 1, NotBefore: nextAttempt}).Error; err != nil {
						return err
					}
					return PendingTasks(tx).Where("next_attempt_at < ?", nextAttempt).Update("next_attempt_at", nextAttempt).Error
				}
				return nil
			}); err != nil {
				return err
			}
			event := "notification_retry"
			task.Attempts, task.LastError = attempts, failure
			if TaskFailed(&task) {
				event = "notification_failed"
			}
			log.Printf("event=%s task_id=%d account_id=%d attempts=%d", event, task.ID, task.AccountID, attempts)
			if limited {
				return nil
			}
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
