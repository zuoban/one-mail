package sync

import (
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

func getBatchSize(isFirstSync bool) int {
	if isFirstSync {
		return 500
	}
	return 100
}

func InitSyncCache() error {
	db := database.GetDB()

	var accounts []models.EmailAccount
	if err := db.Find(&accounts).Error; err != nil {
		return err
	}

	cache := GetSyncCache()

	for _, account := range accounts {
		var messageIDs []string
		if err := db.Model(&models.Email{}).
			Where("account_id = ?", account.ID).
			Pluck("message_id", &messageIDs).Error; err != nil {
			log.Printf("Failed to load message IDs for account %d: %v", account.ID, err)
			continue
		}

		if len(messageIDs) > 0 {
			cache.Rebuild(account.ID, messageIDs)
			log.Printf("Loaded %d message IDs into cache for account %d", len(messageIDs), account.ID)
		}
	}

	return nil
}

func SyncAccount(accountID uint) (int, error) {
	db := database.GetDB()

	var account models.EmailAccount
	if err := db.First(&account, accountID).Error; err != nil {
		return 0, err
	}

	pool := imap.GetConnectionPool()
	client, err := pool.GetConnection(&account)
	if err != nil {
		return 0, err
	}
	defer pool.ReleaseConnection(account.ID)

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
	return totalNew, nil
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

	cache := GetSyncCache()

	var needCheckEmails []*imap.EmailSummary
	for _, e := range emails {
		if !cache.MightContain(account.ID, e.MessageID) {
			needCheckEmails = append(needCheckEmails, e)
		}
	}

	if len(needCheckEmails) == 0 {
		return 0, nil
	}

	messageIDs := make([]string, 0, len(needCheckEmails))
	for _, e := range needCheckEmails {
		messageIDs = append(messageIDs, e.MessageID)
	}

	var existingEmails []models.Email
	db.Where("account_id = ? AND message_id IN ?", account.ID, messageIDs).
		Select("message_id").
		Find(&existingEmails)

	existingSet := make(map[string]bool)
	for _, e := range existingEmails {
		existingSet[e.MessageID] = true
		cache.Add(account.ID, e.MessageID)
	}

	var newEmails []models.Email
	for _, e := range needCheckEmails {
		if existingSet[e.MessageID] {
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
			HasAttachment: e.HasAttachment,
			IsRead:        e.IsRead,
			IsStarred:     e.IsStarred,
			Folder:        folder,
			UID:           e.UID,
		}
		newEmails = append(newEmails, email)
		cache.Add(account.ID, e.MessageID)
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
