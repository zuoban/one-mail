package sync

import (
	"context"
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
var lastFlagSyncAt sync.Map // map[folderSyncKey]time.Time
var accountLatency sync.Map // map[uint]time.Duration

const (
	maxBatchesPerSync = 5
	flagSyncInterval  = 5 * time.Minute
	flagSyncWindow    = 200
)

func parseSyncFolders(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	var folders []string
	hasInbox := false
	var inboxName string

	for _, part := range parts {
		folder := strings.TrimSpace(part)
		if folder == "" {
			continue
		}
		key := strings.ToLower(folder)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if key == "inbox" {
			hasInbox = true
			inboxName = folder
			continue
		}
		folders = append(folders, folder)
	}

	if hasInbox {
		return append([]string{inboxName}, folders...)
	}
	if len(folders) == 0 {
		return []string{"INBOX"}
	}
	return folders
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "authentication failed") ||
		strings.Contains(errStr, "invalid credentials") ||
		strings.Contains(errStr, "failed to login") ||
		strings.Contains(errStr, "bad credentials") ||
		strings.Contains(errStr, "application-specific password") ||
		strings.Contains(errStr, "web login required")
}

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

func syncFolderWithTimeout(ctx context.Context, db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string, timeout time.Duration) (int, error) {
	folderCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		newCount int
		err      error
	}

	done := make(chan result, 1)
	go func() {
		newCount, err := SyncFolderWithContext(folderCtx, db, account, client, folder)
		done <- result{newCount: newCount, err: err}
	}()

	select {
	case res := <-done:
		return res.newCount, res.err
	case <-folderCtx.Done():
		client.ForceClose()
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
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
	return SyncAccountWithContext(context.Background(), accountID)
}

func SyncAccountWithContext(ctx context.Context, accountID uint) (int, error) {
	db := database.GetDB()

	var account models.EmailAccount
	if err := db.First(&account, accountID).Error; err != nil {
		return 0, err
	}

	return SyncAccountModelWithContext(ctx, &account)
}

func SyncAccountWithModel(account *models.EmailAccount) (int, error) {
	return SyncAccountModelWithContext(context.Background(), account)
}

func SyncAccountModelWithContext(ctx context.Context, account *models.EmailAccount) (int, error) {
	if account == nil {
		return 0, fmt.Errorf("account is nil")
	}
	db := database.GetDB()
	accountID := account.ID
	folders := parseSyncFolders(account.SyncFolders)

	pool := imap.GetConnectionPool()
	maxRetries := 3
	for retry := 0; retry <= maxRetries; retry++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		client, release, err := pool.AcquireConnection(account)
		if err != nil {
			return 0, err
		}
		healthy := true

		totalNew := 0
		syncedFolders := 0
		var folderErrors []error
		networkErrorOccurred := false
		authErrorOccurred := false

		for _, folder := range folders {
			if err := ctx.Err(); err != nil {
				release(healthy)
				return totalNew, err
			}

			newCount, syncErr := syncFolderWithTimeout(ctx, db, account, client, folder, 2*time.Minute)
			if syncErr != nil {
				log.Printf("Failed to sync folder %s for account %d: %v", folder, accountID, syncErr)
				if isAuthError(syncErr) {
					authErrorOccurred = true
					healthy = false
				} else if isNetworkError(syncErr) || strings.Contains(strings.ToLower(syncErr.Error()), "timeout") {
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

		if authErrorOccurred {
			return totalNew, fmt.Errorf("sync authentication failed: %w", folderErrors[len(folderErrors)-1])
		}

		if networkErrorOccurred && retry < maxRetries {
			backoff := time.Duration(1<<uint(retry)) * time.Second
			log.Printf("Network error occurred, retrying (%d/%d) after %v...", retry+1, maxRetries, backoff)
			select {
			case <-ctx.Done():
				return totalNew, ctx.Err()
			case <-time.After(backoff):
			}
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
	return SyncFolderWithContext(context.Background(), db, account, client, folder)
}

func SyncFolderWithContext(ctx context.Context, db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string) (int, error) {
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

	initialLastUID := syncState.LastUID
	cache := GetSyncCache()
	totalInserted := 0
	var lastRes *imap.IncrementalFetchResult

	for batchIdx := 0; batchIdx < maxBatchesPerSync; batchIdx++ {
		if err := ctx.Err(); err != nil {
			return totalInserted, err
		}

		batchSize := getBatchSize(account.ID, syncState.LastUID == 0)
		fetchStart := time.Now()
		res, err := client.FetchEmailsIncrementalEx(folder, syncState.LastUID, syncState.UIDValidity, batchSize)
		if err != nil {
			db.Model(&syncState).Update("error", err.Error())
			return totalInserted, err
		}
		accountLatency.Store(account.ID, time.Since(fetchStart))
		lastRes = res

		if res.UIDValidityChanged {
			syncState.LastUID = 0
			syncState.UIDValidity = res.UIDValidity
			db.Model(&syncState).Updates(map[string]interface{}{
				"last_uid":     0,
				"uid_validity": res.UIDValidity,
				"error":        "",
			})
		} else if syncState.UIDValidity == 0 && res.UIDValidity != 0 {
			syncState.UIDValidity = res.UIDValidity
			db.Model(&syncState).Update("uid_validity", res.UIDValidity)
		}

		if len(res.Emails) == 0 {
			if res.MaxUID > syncState.LastUID {
				syncState.LastUID = res.MaxUID
				db.Model(&syncState).Updates(map[string]interface{}{
					"last_uid":       res.MaxUID,
					"last_sync_time": time.Now(),
					"error":          "",
				})
			}
			if !res.HasMore {
				break
			}
			continue
		}

		var needCheckEmails []*imap.EmailSummary
		for _, e := range res.Emails {
			if !cache.MightContain(account.ID, e.MessageID) {
				needCheckEmails = append(needCheckEmails, e)
			}
		}

		log.Printf("Filter result: account=%d folder=%s batch=%d need_check=%d total=%d", account.ID, folder, batchIdx+1, len(needCheckEmails), len(res.Emails))

		if len(needCheckEmails) == 0 {
			var recentIDs []string
			limit := 50
			if len(res.Emails) < limit {
				limit = len(res.Emails)
			}
			for i := 0; i < limit; i++ {
				recentIDs = append(recentIDs, res.Emails[i].MessageID)
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
			if !cacheStale && res.MaxUID > syncState.LastUID {
				syncState.LastUID = res.MaxUID
				db.Model(&syncState).Updates(map[string]interface{}{
					"last_uid":       res.MaxUID,
					"last_sync_time": time.Now(),
					"error":          "",
				})
			}
			if !res.HasMore {
				break
			}
			continue
		}

		messageIDs := make([]string, 0, len(needCheckEmails))
		for _, e := range needCheckEmails {
			messageIDs = append(messageIDs, e.MessageID)
		}

		var existingEmails []models.Email
		db.Where("account_id = ? AND message_id IN ?", account.ID, messageIDs).
			Select("message_id").
			Find(&existingEmails)

		existingSet := make(map[string]bool, len(existingEmails))
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
				return totalInserted, err
			}
			totalInserted += len(newEmails)
			log.Printf("Inserted new emails: account=%d folder=%s inserted=%d", account.ID, folder, len(newEmails))

			for i := range newEmails {
				telegram.SendNewEmailAsync(&newEmails[i], account.Email)
			}
		}

		if res.MaxUID > syncState.LastUID {
			syncState.LastUID = res.MaxUID
			db.Model(&syncState).Updates(map[string]interface{}{
				"last_uid":       res.MaxUID,
				"last_sync_time": time.Now(),
				"error":          "",
			})
		}

		if !res.HasMore {
			break
		}
	}

	if ctx.Err() == nil && lastRes != nil {
		if initialLastUID == 0 {
			lastFlagSyncAt.Store(lockKey, time.Now())
			if lastRes.HighestModSeq > 0 && lastRes.HighestModSeq != syncState.HighestModSeq {
				syncState.HighestModSeq = lastRes.HighestModSeq
				db.Model(&syncState).Update("highest_mod_seq", lastRes.HighestModSeq)
			}
		} else if shouldSyncFlags(lockKey, &syncState, lastRes.HighestModSeq) {
			if err := syncRecentFlagsAndDeletions(db, account.ID, client, folder, &syncState, lastRes.HighestModSeq); err != nil {
				log.Printf("Flag/deletion sync skipped or failed for account=%d folder=%s: %v", account.ID, folder, err)
			} else {
				lastFlagSyncAt.Store(lockKey, time.Now())
			}
		}
	}

	log.Printf("Sync folder summary: account=%d folder=%s new=%d last_uid=%d", account.ID, folder, totalInserted, syncState.LastUID)
	return totalInserted, nil
}

func shouldSyncFlags(lockKey folderSyncKey, syncState *models.SyncState, remoteModSeq uint64) bool {
	if syncState.LastUID == 0 {
		return false
	}
	if remoteModSeq > 0 {
		return remoteModSeq != syncState.HighestModSeq
	}
	if v, ok := lastFlagSyncAt.Load(lockKey); ok {
		if time.Since(v.(time.Time)) < flagSyncInterval {
			return false
		}
	}
	return true
}

func syncRecentFlagsAndDeletions(db *gorm.DB, accountID uint, client *imap.Client, folder string, syncState *models.SyncState, remoteModSeq uint64) error {
	type localEmailState struct {
		ID        uint
		UID       uint
		IsRead    bool
		IsStarred bool
	}

	var locals []localEmailState
	if err := db.Model(&models.Email{}).
		Select("id, uid, is_read, is_starred").
		Where("account_id = ? AND folder = ? AND uid > 0", accountID, folder).
		Order("uid DESC").
		Limit(flagSyncWindow).
		Scan(&locals).Error; err != nil {
		return err
	}
	if len(locals) == 0 {
		if remoteModSeq > 0 && remoteModSeq != syncState.HighestModSeq {
			syncState.HighestModSeq = remoteModSeq
			db.Model(syncState).Update("highest_mod_seq", remoteModSeq)
		}
		return nil
	}

	minUID := locals[len(locals)-1].UID
	maxUID := locals[0].UID
	flagsByUID, numMessages, err := client.FetchRecentFlags(folder, minUID, maxUID)
	if err != nil {
		return err
	}

	// Avoid accidental mass soft-delete if a quirky server returns empty FETCH when mailbox still has messages.
	canReconcileDeletions := len(flagsByUID) > 0 || numMessages == 0
	if !canReconcileDeletions {
		return nil
	}

	var markReadIDs, markUnreadIDs, starIDs, unstarIDs, deletedIDs []uint
	for _, local := range locals {
		remote, exists := flagsByUID[local.UID]
		if !exists {
			deletedIDs = append(deletedIDs, local.ID)
			continue
		}
		if remote.IsRead != local.IsRead {
			if remote.IsRead {
				markReadIDs = append(markReadIDs, local.ID)
			} else {
				markUnreadIDs = append(markUnreadIDs, local.ID)
			}
		}
		if remote.IsStarred != local.IsStarred {
			if remote.IsStarred {
				starIDs = append(starIDs, local.ID)
			} else {
				unstarIDs = append(unstarIDs, local.ID)
			}
		}
	}

	if len(markReadIDs) > 0 {
		db.Model(&models.Email{}).Where("id IN ?", markReadIDs).Update("is_read", true)
	}
	if len(markUnreadIDs) > 0 {
		db.Model(&models.Email{}).Where("id IN ?", markUnreadIDs).Update("is_read", false)
	}
	if len(starIDs) > 0 {
		db.Model(&models.Email{}).Where("id IN ?", starIDs).Update("is_starred", true)
	}
	if len(unstarIDs) > 0 {
		db.Model(&models.Email{}).Where("id IN ?", unstarIDs).Update("is_starred", false)
	}
	if len(deletedIDs) > 0 {
		db.Where("id IN ?", deletedIDs).Delete(&models.Email{})
	}

	if remoteModSeq > 0 && remoteModSeq != syncState.HighestModSeq {
		syncState.HighestModSeq = remoteModSeq
		db.Model(syncState).Update("highest_mod_seq", remoteModSeq)
	}

	if len(markReadIDs)+len(markUnreadIDs)+len(starIDs)+len(unstarIDs)+len(deletedIDs) > 0 {
		log.Printf("Reconciled flags/deletions: account=%d folder=%s read=%d unread=%d star=%d unstar=%d deleted=%d",
			accountID, folder, len(markReadIDs), len(markUnreadIDs), len(starIDs), len(unstarIDs), len(deletedIDs))
	}
	return nil
}
