package telegram

import (
	"strings"

	"one-mail/backend/internal/models"

	"gorm.io/gorm"
)

const MaxDeliveryAttempts = 10

const permanentFailurePrefix = "permanent: "

func TaskFailed(task *models.NotificationTask) bool {
	return task.Attempts >= MaxDeliveryAttempts || strings.HasPrefix(task.LastError, permanentFailurePrefix)
}

func PendingTasks(db *gorm.DB) *gorm.DB {
	return db.Model(&models.NotificationTask{}).
		Where("attempts < ? AND COALESCE(last_error, '') NOT LIKE ?", MaxDeliveryAttempts, permanentFailurePrefix+"%")
}

func FailedTasks(db *gorm.DB) *gorm.DB {
	return db.Model(&models.NotificationTask{}).
		Where("(attempts >= ? OR COALESCE(last_error, '') LIKE ?)", MaxDeliveryAttempts, permanentFailurePrefix+"%")
}
