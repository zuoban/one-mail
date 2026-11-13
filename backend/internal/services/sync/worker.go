package sync

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/telegram"
)


type folderSyncKey struct {
	accountID uint
	folder    string
}

var inFlightFolders sync.Map
var accountLatency sync.Map // map[uint]time.Duration

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "connection timed out") ||
		strings.Contains(errStr, "bad connection")
}

func getBatchSize(accountID uint, isFirstSync bool) int {
	if isFirstSync {
		return 500
	}
	if v, ok := accountLatency.Load(accountID); ok {
		latency := v.(time.Duration)
		if latency > 3*time.Second {
			return 50
		}
		if latency > 1*time.Second {
			return 100
		}
		return 200
	}
	return 100
}

func syncFolderWithTimeout(db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string, timeout time.Duration) (int, error) {
	type result struct {
		newCount int
		err      error
	}

	done := make(chan result, 1)
	go func() {
		newCount, err := SyncFolder(db, account, client, folder)
		done <- result{newCount: newCount, err: err}
	}()

	select {
	case res := <-done:
		return res.newCount, res.err
	case <-time.After(timeout):
		client.ForceClose()
		return 0, fmt.Errorf("sync folder timeout after %v", timeout)
	}
}

func rebuildAccountCacheBatched(db *gorm.DB, cache *SyncCache, accountID uint) (int64, error) {
	var total int64
	if err := db.Model(&models.Email{}).Where("account_id = ?", accountID).Count(&total).Error; err != nil {
		return 0, err
	}
	if total == 0 {
		cache.Clear(accountID)
		return 0, nil
	}

	filter := NewDetachedFilter(uint(total))
	type idMsgRow struct {
		ID        uint
		MessageID string
	}

	const chunkSize = 5000
	var lastID uint
	for {
		var rows []idMsgRow
		if err := db.Model(&models.Email{}).
			Select("id, message_id").
			Where("account_id = ? AND id > ?", accountID, lastID).
			Order("id ASC").
			Limit(chunkSize).
			Scan(&rows).Error; err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if row.MessageID != "" {
				filter.AddString(row.MessageID)
			}
			lastID = row.ID
		}
		if len(rows) < chunkSize {
			break
		}
	}
	cache.SwapFilter(accountID, filter)
	return total, nil
}

func InitSyncCache() error {
	db := database.GetDB()

	var accounts []models.EmailAccount
	if err := db.Select("id").Find(&accounts).Error; err != nil {
		return err
	}

	cache := GetSyncCache()

	for _, account := range accounts {
		total, err := rebuildAccountCacheBatched(db, cache, account.ID)
		if err != nil {
			log.Printf("Failed to load message IDs for account %d: %v", account.ID, err)
			continue
		}
		if total > 0 {
			log.Printf("Loaded %d message IDs into cache for account %d", total, account.ID)
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

	return SyncAccountWithModel(&account)
}

func SyncAccountWithModel(account *models.EmailAccount) (int, error) {
	if account == nil {
		return 0, fmt.Errorf("account is nil")
	}
	db := database.GetDB()
	accountID := account.ID
	folders := strings.Split(account.SyncFolders, ",")
	if len(folders) == 0 {
		folders = []string{"INBOX"}
	}

	pool := imap.GetConnectionPool()
	maxRetries := 3
	for retry := 0; retry <= maxRetries; retry++ {
		client, release, err := pool.AcquireConnection(account)
		if err != nil {
			return 0, err
		}
		healthy := true

		totalNew := 0
		syncedFolders := 0
		var folderErrors []error
		networkErrorOccurred := false

		for _, folder := range folders {
			folder = strings.TrimSpace(folder)
			if folder == "" {
				continue
			}

			newCount, syncErr := syncFolderWithTimeout(db, account, client, folder, 2*time.Minute)
			if syncErr != nil {
				log.Printf("Failed to sync folder %s for account %d: %v", folder, accountID, syncErr)
				if isNetworkError(syncErr) || strings.Contains(strings.ToLower(syncErr.Error()), "timeout") {
					networkErrorOccurred = true
					healthy = false
				}
				folderErrors = append(folderErrors, syncErr)
				continue
			}
			syncedFolders++
			totalNew += newCount
		}

		release(healthy)
		db.Model(account).Update("last_sync_time", time.Now())

		if syncedFolders > 0 {
			log.Printf("Synced account %d, %d new emails", accountID, totalNew)
			return totalNew, nil
		}

		if networkErrorOccurred && retry < maxRetries {
			backoff := time.Duration(1<<uint(retry)) * time.Second
			log.Printf("Network error occurred, retrying (%d/%d) after %v...", retry+1, maxRetries, backoff)
			time.Sleep(backoff)
			continue
		}

		if syncedFolders == 0 && len(folderErrors) > 0 {
			return totalNew, fmt.Errorf("sync failed for all folders (last error: %w)", folderErrors[len(folderErrors)-1])
		}

		return totalNew, nil
	}

	return 0, fmt.Errorf("sync failed after %d retries", maxRetries)
}

func SyncFolder(db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string) (int, error) {
	if client == nil {
		return 0, fmt.Errorf("client not connected")
	}

	lockKey := folderSyncKey{accountID: account.ID, folder: folder}
	if _, loaded := inFlightFolders.LoadOrStore(lockKey, struct{}{}); loaded {
		log.Printf("Skip sync due to running lock: account=%d folder=%s", account.ID, folder)
		return 0, nil
	}
	defer inFlightFolders.Delete(lockKey)

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
	batchSize := getBatchSize(account.ID, syncState.LastUID == 0)
	fetchStart := time.Now()
	emails, maxUID, err := client.FetchEmailsIncremental(folder, syncState.LastUID, batchSize)
	if err != nil {
		db.Model(&syncState).Update("error", err.Error())
		return 0, err
	}
	accountLatency.Store(account.ID, time.Since(fetchStart))

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

		cacheStale := false
		if len(recentIDs) > 0 {
			var existingCount int64
			db.Model(&models.Email{}).
				Where("account_id = ? AND message_id IN ?", account.ID, recentIDs).
				Count(&existingCount)
			if existingCount < int64(len(recentIDs)) {
				cacheStale = true
				log.Printf("Bloom filter may be stale, rebuild cache: account=%d folder=%s checked=%d exists=%d", account.ID, folder, len(recentIDs), existingCount)
				if _, err := rebuildAccountCacheBatched(db, cache, account.ID); err != nil {
					log.Printf("Failed to rebuild cache for account %d: %v", account.ID, err)
				}
			}
		}
		if !cacheStale && maxUID > syncState.LastUID {
			db.Model(&syncState).Updates(map[string]interface{}{
				"last_uid":       maxUID,
				"last_sync_time": time.Now(),
				"error":          "",
			})
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
		if err := db.CreateInBatches(&newEmails, 100).Error; err != nil {
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
