package sync

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/telegram"
)

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "network is unreachable")
}

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

	log.Printf("Sync account %d folders: %v", accountID, folders)

	maxRetries := 2
	for retry := 0; retry <= maxRetries; retry++ {
		totalNew := 0
		syncedFolders := 0
		var folderErrors []error
		networkErrorOccurred := false

		for _, folder := range folders {
			folder = strings.TrimSpace(folder)
			if folder == "" {
				continue
			}

			log.Printf("Sync folder start: account=%d folder=%s", accountID, folder)
			newCount, err := SyncFolder(db, &account, client, folder)
			if err != nil {
				log.Printf("Failed to sync folder %s for account %d: %v", folder, accountID, err)
				if isNetworkError(err) {
					networkErrorOccurred = true
				}
				folderErrors = append(folderErrors, err)
				continue
			}
			log.Printf("Sync folder done: account=%d folder=%s new_count=%d", accountID, folder, newCount)
			syncedFolders++
			totalNew += newCount
		}

		db.Model(&account).Update("last_sync_time", time.Now())

		if syncedFolders > 0 {
			log.Printf("Synced account %d, %d new emails", accountID, totalNew)
			return totalNew, nil
		}

		if networkErrorOccurred && retry < maxRetries {
			log.Printf("Network error occurred, retrying (%d/%d)...", retry+1, maxRetries)
			pool.RemoveConnection(account.ID)
			client, err = pool.GetConnection(&account)
			if err != nil {
				return 0, err
			}
			time.Sleep(time.Second * 2)
			continue
		}

		if syncedFolders == 0 && len(folderErrors) > 0 {
			return totalNew, fmt.Errorf("sync failed for all folders (last error: %w)", folderErrors[len(folderErrors)-1])
		}

		log.Printf("Synced account %d, %d new emails", accountID, totalNew)
		return totalNew, nil
	}

	return 0, fmt.Errorf("sync failed after %d retries", maxRetries)
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
		staleFor := time.Since(isSyncing.UpdatedAt)
		if staleFor > 10*time.Minute {
			log.Printf("Sync lock stale, reset: account=%d folder=%s stale_for=%v", account.ID, folder, staleFor)
			db.Model(&syncState).Updates(map[string]interface{}{
				"is_syncing": false,
				"error":      "stale sync lock reset",
			})
		} else {
			log.Printf("Skip sync due to running lock: account=%d folder=%s running_for=%v", account.ID, folder, staleFor)
			return 0, nil
		}
	}

	log.Printf("Sync folder state: account=%d folder=%s last_uid=%d", account.ID, folder, syncState.LastUID)

	db.Model(&syncState).Update("is_syncing", true)
	defer db.Model(&syncState).Update("is_syncing", false)

	batchSize := getBatchSize(syncState.LastUID == 0)
	emails, maxUID, err := client.FetchEmailsIncremental(folder, syncState.LastUID, batchSize)
	if err != nil {
		db.Model(&syncState).Update("error", err.Error())
		return 0, err
	}

	log.Printf("Fetch incremental: account=%d folder=%s emails=%d max_uid=%d", account.ID, folder, len(emails), maxUID)

	if len(emails) == 0 {
		status, statusErr := client.Status(folder)
		if statusErr == nil && status != nil && status.UIDNext > 0 {
			uidNext := uint(status.UIDNext)
			if uidNext > syncState.LastUID {
				windowSize := uint(200)
				fallbackStart := uint(1)
				if uidNext > windowSize {
					fallbackStart = uidNext - windowSize
				}
				fallbackLastUID := fallbackStart - 1
				log.Printf("Incremental empty but UIDNext ahead, fallback fetch: account=%d folder=%s last_uid=%d uid_next=%d fallback_start=%d", account.ID, folder, syncState.LastUID, uidNext, fallbackStart)
				fallbackEmails, fallbackMaxUID, fallbackErr := client.FetchEmailsIncremental(folder, fallbackLastUID, int(windowSize))
				if fallbackErr == nil {
					emails = fallbackEmails
					maxUID = fallbackMaxUID
					log.Printf("Fallback fetch done: account=%d folder=%s emails=%d max_uid=%d", account.ID, folder, len(emails), maxUID)
				} else {
					log.Printf("Fallback fetch failed: account=%d folder=%s error=%v", account.ID, folder, fallbackErr)
				}
			}
		}
		if len(emails) == 0 {
			return 0, nil
		}
	}

	cache := GetSyncCache()

	var needCheckEmails []*imap.EmailSummary
	for _, e := range emails {
		if !cache.MightContain(account.ID, e.MessageID) {
			needCheckEmails = append(needCheckEmails, e)
		}
	}

	log.Printf("Filter result: account=%d folder=%s need_check=%d total=%d", account.ID, folder, len(needCheckEmails), len(emails))

	if len(needCheckEmails) == 0 {
		var recentIDs []string
		limit := 50
		if len(emails) < limit {
			limit = len(emails)
		}
		for i := 0; i < limit; i++ {
			recentIDs = append(recentIDs, emails[i].MessageID)
		}

		if len(recentIDs) > 0 {
			var existingCount int64
			db.Model(&models.Email{}).
				Where("account_id = ? AND message_id IN ?", account.ID, recentIDs).
				Count(&existingCount)
			if existingCount < int64(len(recentIDs)) {
				log.Printf("Bloom filter may be stale, rebuild cache: account=%d folder=%s checked=%d exists=%d", account.ID, folder, len(recentIDs), existingCount)
				var allIDs []string
				if err := db.Model(&models.Email{}).
					Where("account_id = ?", account.ID).
					Pluck("message_id", &allIDs).Error; err == nil {
					cache.Rebuild(account.ID, allIDs)
				}
			}
		}
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

	log.Printf("New emails prepared: account=%d folder=%s new_count=%d", account.ID, folder, len(newEmails))

	if len(newEmails) > 0 {
		if err := db.Create(&newEmails).Error; err != nil {
			return 0, err
		}
		log.Printf("Inserted new emails: account=%d folder=%s inserted=%d", account.ID, folder, len(newEmails))

		for i := range newEmails {
			telegram.SendNewEmailAsync(&newEmails[i], account.Email)
		}
	}

	if maxUID > syncState.LastUID {
		db.Model(&syncState).Updates(map[string]interface{}{
			"last_uid":       maxUID,
			"last_sync_time": time.Now(),
			"error":          "",
		})
	}

	log.Printf("Sync folder summary: account=%d folder=%s new=%d last_uid=%d max_uid=%d", account.ID, folder, len(newEmails), syncState.LastUID, maxUID)

	return len(newEmails), nil
}
