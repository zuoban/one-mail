package sync

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

type SyncStatus struct {
	AccountID    uint      `json:"account_id"`
	Running      bool      `json:"running"`
	LastSyncTime time.Time `json:"last_sync_time"`
	Error        string    `json:"error"`
	NewCount     int       `json:"new_count"`
}

type Worker struct {
	accountID uint
	interval  time.Duration
	status    *SyncStatus
	mu        sync.RWMutex
	stopChan  chan struct{}
	trigger   chan struct{}
}

func NewWorker(accountID uint, interval time.Duration) *Worker {
	return &Worker{
		accountID: accountID,
		interval:  interval,
		status: &SyncStatus{
			AccountID: accountID,
			Running:   false,
		},
		stopChan: make(chan struct{}),
		trigger:  make(chan struct{}, 1),
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.doSync()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-w.trigger:
			w.doSync()
		case <-ticker.C:
			w.doSync()
		}
	}
}

func (w *Worker) Stop() {
	close(w.stopChan)
}

func (w *Worker) TriggerSync() error {
	select {
	case w.trigger <- struct{}{}:
	default:
	}
	return nil
}

func (w *Worker) UpdateInterval(interval time.Duration) {
	w.mu.Lock()
	w.interval = interval
	w.mu.Unlock()
}

func (w *Worker) GetStatus() *SyncStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) doSync() {
	w.mu.Lock()
	if w.status.Running {
		w.mu.Unlock()
		return
	}
	w.status.Running = true
	w.status.Error = ""
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.status.Running = false
		w.mu.Unlock()
	}()

	err := SyncAccount(w.accountID)

	w.mu.Lock()
	if err != nil {
		w.status.Error = err.Error()
		log.Printf("Sync failed for account %d: %v", w.accountID, err)
	} else {
		w.status.LastSyncTime = time.Now()
	}
	w.mu.Unlock()
}

func SyncAccount(accountID uint) error {
	db := database.GetDB()

	var account models.EmailAccount
	if err := db.First(&account, accountID).Error; err != nil {
		return err
	}

	client := imap.NewClient(&account)
	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Disconnect()

	folders := strings.Split(account.SyncFolders, ",")
	if len(folders) == 0 {
		folders = []string{"INBOX"}
	}

	totalNew := 0
	for _, folder := range folders {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			continue
		}

		newCount, err := SyncFolder(db, &account, client, folder)
		if err != nil {
			log.Printf("Failed to sync folder %s for account %d: %v", folder, accountID, err)
			continue
		}
		totalNew += newCount
	}

	db.Model(&account).Update("last_sync_time", time.Now())

	log.Printf("Synced account %d, %d new emails", accountID, totalNew)
	return nil
}

func SyncFolder(db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string) (int, error) {
	var syncState models.SyncState
	result := db.Where("account_id = ? AND folder = ?", account.ID, folder).First(&syncState)
	if result.Error != nil {
		syncState = models.SyncState{
			AccountID: account.ID,
			Folder:    folder,
			LastUID:   0,
		}
		db.Create(&syncState)
	}

	var isSyncing models.SyncState
	if err := db.Where("id = ?", syncState.ID).First(&isSyncing).Error; err == nil && isSyncing.IsSyncing {
		return 0, nil
	}

	db.Model(&syncState).Update("is_syncing", true)
	defer db.Model(&syncState).Update("is_syncing", false)

	emails, maxUID, err := client.FetchEmailsIncremental(folder, syncState.LastUID, 100)
	if err != nil {
		db.Model(&syncState).Update("error", err.Error())
		return 0, err
	}

	if len(emails) == 0 {
		return 0, nil
	}

	var newEmails []models.Email
	for _, e := range emails {
		var existing models.Email
		result := db.Where("account_id = ? AND message_id = ?", account.ID, e.MessageID).First(&existing)
		if result.Error == nil {
			continue
		}

		email := models.Email{
			AccountID:     account.ID,
			MessageID:     e.MessageID,
			From:          e.From,
			FromName:      e.FromName,
			To:            e.To,
			Subject:       e.Subject,
			Date:          e.Date,
			BodyText:      e.BodyText,
			BodyHTML:      e.BodyHTML,
			HasAttachment: e.HasAttachment,
			IsRead:        e.IsRead,
			IsStarred:     e.IsStarred,
			Folder:        folder,
			UID:           e.UID,
		}
		newEmails = append(newEmails, email)
	}

	if len(newEmails) > 0 {
		if err := db.Create(&newEmails).Error; err != nil {
			return 0, err
		}
	}

	if maxUID > syncState.LastUID {
		db.Model(&syncState).Updates(map[string]interface{}{
			"last_uid":       maxUID,
			"last_sync_time": time.Now(),
			"error":          "",
		})
	}

	return len(newEmails), nil
}
