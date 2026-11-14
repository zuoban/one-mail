package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
	Username       string         `gorm:"unique;not null" json:"username"`
	Password       string         `gorm:"not null" json:"-"`
	SessionVersion uint           `gorm:"not null;default:1" json:"-"`
	Email          string         `json:"email"`
	Accounts       []EmailAccount `gorm:"foreignKey:UserID" json:"accounts,omitempty"`
}

type EmailAccount struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
	UserID       uint           `gorm:"not null;index" json:"user_id"`
	Email        string         `gorm:"not null" json:"email"`
	Provider     string         `gorm:"not null" json:"provider"`
	IMAPHost     string         `gorm:"not null" json:"imap_host"`
	IMAPPort     int            `gorm:"not null" json:"imap_port"`
	SMTPHost     string         `gorm:"not null" json:"smtp_host"`
	SMTPPort     int            `gorm:"not null" json:"smtp_port"`
	Username     string         `gorm:"not null" json:"username"`
	Password     string         `gorm:"not null" json:"-"`
	LastSyncTime time.Time      `json:"last_sync_time"`
	UIDValidity  uint32         `gorm:"not null;default:0" json:"-"`
	Status       string         `gorm:"default:'active'" json:"status"`
	Color        string         `gorm:"default:'#6366f1'" json:"color"`
	Emails       []Email        `gorm:"foreignKey:AccountID" json:"emails,omitempty"`
}

type Email struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
	AccountID     uint           `gorm:"not null;index:idx_account_message,priority:1;index:idx_account_folder_date,priority:1;index:idx_account_folder_read_date,priority:1" json:"account_id"`
	MessageID     string         `gorm:"not null;index:idx_account_message,priority:2" json:"message_id"`
	From          string         `gorm:"not null" json:"from"`
	FromName      string         `json:"from_name"`
	To            string         `gorm:"not null" json:"to"`
	Subject       string         `json:"subject"`
	Date          time.Time      `gorm:"index:idx_account_folder_date,priority:3;index:idx_account_folder_read_date,priority:4" json:"date"`
	BodyText      string         `gorm:"type:text" json:"body_text"`
	BodyHTML      string         `gorm:"type:text" json:"body_html"`
	BodyFetched   bool           `gorm:"not null;default:false" json:"-"`
	HasAttachment bool           `gorm:"default:false" json:"has_attachment"`
	IsRead        bool           `gorm:"default:false;index:idx_account_folder_read_date,priority:3" json:"is_read"`
	PendingRead   *bool          `json:"-"`
	IsStarred     bool           `gorm:"default:false;index" json:"is_starred"`
	Folder        string         `gorm:"default:'INBOX';index:idx_account_folder_date,priority:2;index:idx_account_folder_read_date,priority:2;index" json:"folder"`
	UID           uint           `gorm:"not null;index" json:"uid"`
	UIDValidity   uint32         `gorm:"not null;default:0" json:"-"`
}

type TelegramConfig struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"uniqueIndex:idx_telegram_user,where:user_id > 0" json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	BotToken  string    `gorm:"type:text" json:"bot_token"`
	ChatID    string    `gorm:"type:text" json:"chat_id"`
}
