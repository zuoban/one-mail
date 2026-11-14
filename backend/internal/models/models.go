package models

import (
	"time"

	"gorm.io/gorm"
)

type EmailAccount struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
	Email           string         `gorm:"not null" json:"email"`
	Provider        string         `gorm:"not null" json:"provider"`
	IMAPHost        string         `gorm:"not null" json:"imap_host"`
	IMAPPort        int            `gorm:"not null" json:"imap_port"`
	SMTPHost        string         `gorm:"not null" json:"smtp_host"`
	SMTPPort        int            `gorm:"not null" json:"smtp_port"`
	Username        string         `gorm:"not null" json:"username"`
	Password        string         `gorm:"not null" json:"-"`
	LastSyncTime    time.Time      `json:"last_sync_time"`
	UIDValidity     uint32         `gorm:"not null;default:0" json:"-"`
	IdentityVersion uint64         `gorm:"not null;default:0" json:"-"`
	ConfigVersion   uint64         `gorm:"not null;default:0" json:"-"`
	Status          string         `gorm:"default:'active'" json:"status"`
	Color           string         `gorm:"default:'#6366f1'" json:"color"`
	Emails          []Email        `gorm:"foreignKey:AccountID" json:"emails,omitempty"`
}

type Email struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	AccountID       uint      `gorm:"not null;index:idx_account_message,priority:1;index:idx_account_folder_date,priority:1" json:"account_id"`
	MessageID       string    `gorm:"not null;index:idx_account_message,priority:2" json:"message_id"`
	From            string    `gorm:"not null" json:"from"`
	FromName        string    `json:"from_name"`
	To              string    `gorm:"not null" json:"to"`
	Subject         string    `json:"subject"`
	Date            time.Time `gorm:"index:idx_account_folder_date,priority:3" json:"date"`
	BodyText        string    `gorm:"type:text" json:"body_text"`
	BodyHTML        string    `gorm:"type:text" json:"body_html"`
	BodyFetched     bool      `gorm:"not null;default:false" json:"-"`
	HasAttachment   bool      `gorm:"default:false" json:"has_attachment"`
	Folder          string    `gorm:"default:'INBOX';index:idx_account_folder_date,priority:2;index" json:"folder"`
	UID             uint      `gorm:"not null;index" json:"uid"`
	UIDValidity     uint32    `gorm:"not null;default:0" json:"-"`
	IdentityVersion uint64    `gorm:"not null;default:0" json:"-"`
}

type TelegramConfig struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	BotToken  string    `gorm:"type:text" json:"bot_token"`
	ChatID    string    `gorm:"type:text" json:"chat_id"`
}

// NotificationTask stores a delivery snapshot independently of the recent-mail
// window, so cache eviction cannot discard a committed notification.
type NotificationTask struct {
	ID              uint `gorm:"primaryKey"`
	CreatedAt       time.Time
	AccountID       uint      `gorm:"not null;index"`
	IdentityVersion uint64    `gorm:"not null"`
	MessageKey      string    `gorm:"not null;uniqueIndex"`
	AccountEmail    string    `gorm:"not null"`
	EmailJSON       string    `gorm:"not null;type:text"`
	Attempts        uint      `gorm:"not null;default:0"`
	NextAttemptAt   time.Time `gorm:"not null;index"`
	LastError       string    `gorm:"type:text"`
}
