package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Username  string         `gorm:"unique;not null" json:"username"`
	Password  string         `gorm:"not null" json:"-"`
	Email     string         `json:"email"`
	Accounts  []EmailAccount `gorm:"foreignKey:UserID" json:"accounts,omitempty"`
}

type EmailAccount struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
	UserID         uint           `gorm:"not null;index" json:"user_id"`
	Email          string         `gorm:"not null" json:"email"`
	Provider       string         `gorm:"not null" json:"provider"`
	IMAPHost       string         `gorm:"not null" json:"imap_host"`
	IMAPPort       int            `gorm:"not null" json:"imap_port"`
	SMTPHost       string         `gorm:"not null" json:"smtp_host"`
	SMTPPort       int            `gorm:"not null" json:"smtp_port"`
	Username       string         `gorm:"not null" json:"username"`
	Password       string         `gorm:"not null" json:"-"`
	LastSyncTime   time.Time      `json:"last_sync_time"`
	Status         string         `gorm:"default:'active'" json:"status"`
	Color          string         `gorm:"default:'#6366f1'" json:"color"`
	SyncInterval   int            `gorm:"default:1" json:"sync_interval"`
	SyncFolders    string         `gorm:"default:'INBOX'" json:"sync_folders"`
	EnableAutoSync bool           `gorm:"default:true" json:"enable_auto_sync"`
	Emails         []Email        `gorm:"foreignKey:AccountID" json:"emails,omitempty"`
	SyncStates     []SyncState    `gorm:"foreignKey:AccountID" json:"sync_states,omitempty"`
}

type SyncState struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	AccountID    uint      `gorm:"not null;index" json:"account_id"`
	Folder       string    `gorm:"not null;index" json:"folder"`
	LastUID      uint      `gorm:"default:0" json:"last_uid"`
	LastSyncTime time.Time `json:"last_sync_time"`
	IsSyncing    bool      `gorm:"default:false" json:"is_syncing"`
	Error        string    `gorm:"type:text" json:"error"`
}

type SyncLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	AccountID  uint      `gorm:"not null;index" json:"account_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Status     string    `gorm:"not null" json:"status"`
	NewCount   int       `gorm:"default:0" json:"new_count"`
	Error      string    `gorm:"type:text" json:"error"`
	DurationMs int64     `json:"duration_ms"`
}

type Email struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
	AccountID     uint           `gorm:"not null;index:idx_account_message,priority:1;index:idx_account_folder_date,priority:1;index:idx_account_read,priority:1" json:"account_id"`
	MessageID     string         `gorm:"not null;index:idx_account_message,priority:2" json:"message_id"`
	From          string         `gorm:"not null" json:"from"`
	FromName      string         `json:"from_name"`
	To            string         `gorm:"not null" json:"to"`
	Subject       string         `json:"subject"`
	Date          time.Time      `gorm:"index:idx_account_folder_date,priority:2" json:"date"`
	BodyText      string         `gorm:"type:text" json:"body_text"`
	BodyHTML      string         `gorm:"type:text" json:"body_html"`
	HasAttachment bool           `gorm:"default:false" json:"has_attachment"`
	IsRead        bool           `gorm:"default:false;index:idx_account_read,priority:2" json:"is_read"`
	IsStarred     bool           `gorm:"default:false;index" json:"is_starred"`
	Folder        string         `gorm:"default:'INBOX';index:idx_account_folder_date,priority:3;index" json:"folder"`
	UID           uint           `gorm:"not null;index" json:"uid"`
}

type TelegramConfig struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	BotToken  string    `gorm:"type:text" json:"bot_token"`
	ChatID    string    `gorm:"type:text" json:"chat_id"`
}
