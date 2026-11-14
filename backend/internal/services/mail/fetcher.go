package mail

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/telegram"
)

const (
	MaxLatestEmails   = 50
	DefaultFolder     = "INBOX"
	refreshTimeout    = 20 * time.Second
	maxRefreshWorkers = 4
)

type RefreshSummary struct {
	Refreshed int               `json:"refreshed_accounts"`
	NewCount  int               `json:"new_count"`
	Errors    map[string]string `json:"errors,omitempty"`
}

// RefreshAccount fetches the latest `limit` emails from IMAP for the account and reconciles them into DB.
func RefreshAccount(ctx context.Context, db *gorm.DB, account *models.EmailAccount, limit int) (int, error) {
	if account == nil {
		return 0, fmt.Errorf("account is nil")
	}
	if limit <= 0 {
		limit = MaxLatestEmails
	}

	reqCtx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	client, release, err := imap.GetConnectionPool().AcquireConnectionContext(reqCtx, account)
	if err != nil {
		return 0, err
	}
	healthy := true
	defer func() {
		release(healthy)
	}()

	type fetchResult struct {
		emails []*imap.EmailSummary
		err    error
	}
	done := make(chan fetchResult, 1)
	go func() {
		emails, fetchErr := client.FetchLatestEmails(DefaultFolder, limit)
		done <- fetchResult{emails: emails, err: fetchErr}
	}()

	var res fetchResult
	select {
	case res = <-done:
	case <-reqCtx.Done():
		healthy = false
		client.ForceClose()
		return 0, fmt.Errorf("fetch latest emails timeout: %w", reqCtx.Err())
	}

	if res.err != nil {
		healthy = false
		return 0, res.err
	}

	return ReconcileLatestEmails(db, account, DefaultFolder, res.emails, limit)
}

// ReconcileLatestEmails inserts new emails, updates read/starred flags for existing ones,
// and prunes older records so each account+folder stores at most `limit` emails.
func ReconcileLatestEmails(db *gorm.DB, account *models.EmailAccount, folder string, summaries []*imap.EmailSummary, limit int) (int, error) {
	if limit <= 0 {
		limit = MaxLatestEmails
	}
	if folder == "" {
		folder = DefaultFolder
	}

	messageIDs := make([]string, len(summaries))
	uids := make([]uint, 0, len(summaries))
	for i, summary := range summaries {
		if summary == nil {
			continue
		}
		messageIDs[i] = summary.MessageID
		if messageIDs[i] == "" {
			messageIDs[i] = fmt.Sprintf("uid-%d", summary.UID)
		}
		if summary.UID > 0 {
			uids = append(uids, summary.UID)
		}
	}

	type existingRow struct {
		ID        uint
		MessageID string
		UID       uint
		IsRead    bool
		IsStarred bool
		DeletedAt gorm.DeletedAt
	}

	var newEmails []models.Email
	var hadExisting bool
	syncTime := time.Now()
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing []existingRow
		if err := tx.Unscoped().Model(&models.Email{}).
			Select("id, message_id, uid, is_read, is_starred, deleted_at").
			Where("account_id = ? AND folder = ?", account.ID, folder).
			Where("message_id IN ? OR uid IN ?", messageIDs, uids).
			Scan(&existing).Error; err != nil {
			return err
		}

		hadExisting = len(existing) > 0
		if !hadExisting {
			var prior []existingRow
			if err := tx.Unscoped().Model(&models.Email{}).
				Select("id").Where("account_id = ? AND folder = ?", account.ID, folder).
				Limit(1).Find(&prior).Error; err != nil {
				return err
			}
			hadExisting = len(prior) > 0
		}
		byMsgID := make(map[string]existingRow, len(existing))
		byUID := make(map[uint]existingRow, len(existing))
		for _, row := range existing {
			if row.MessageID != "" {
				byMsgID[row.MessageID] = row
			}
			if row.UID > 0 {
				byUID[row.UID] = row
			}
		}

		var (
			markReadIDs   []uint
			markUnreadIDs []uint
			starIDs       []uint
			unstarIDs     []uint
		)
		seenIncoming := make(map[string]struct{}, len(summaries))
		for i, s := range summaries {
			if s == nil {
				continue
			}
			msgID := messageIDs[i]
			if _, dup := seenIncoming[msgID]; dup {
				continue
			}
			seenIncoming[msgID] = struct{}{}

			row, found := byMsgID[msgID]
			if !found && s.UID > 0 {
				row, found = byUID[s.UID]
			}
			if found {
				if row.DeletedAt.Valid {
					continue
				}
				if s.IsRead != row.IsRead {
					if s.IsRead {
						markReadIDs = append(markReadIDs, row.ID)
					} else {
						markUnreadIDs = append(markUnreadIDs, row.ID)
					}
				}
				if s.IsStarred != row.IsStarred {
					if s.IsStarred {
						starIDs = append(starIDs, row.ID)
					} else {
						unstarIDs = append(unstarIDs, row.ID)
					}
				}
				continue
			}

			newEmails = append(newEmails, models.Email{
				AccountID:     account.ID,
				MessageID:     msgID,
				From:          s.From,
				FromName:      s.FromName,
				To:            s.To,
				Subject:       s.Subject,
				Date:          s.Date,
				HasAttachment: s.HasAttachment,
				IsRead:        s.IsRead,
				IsStarred:     s.IsStarred,
				Folder:        folder,
				UID:           s.UID,
			})
		}

		if len(newEmails) > 0 {
			if err := tx.CreateInBatches(&newEmails, 50).Error; err != nil {
				return err
			}
		}
		if len(markReadIDs) > 0 {
			if err := tx.Model(&models.Email{}).Where("id IN ?", markReadIDs).Update("is_read", true).Error; err != nil {
				return err
			}
		}
		if len(markUnreadIDs) > 0 {
			if err := tx.Model(&models.Email{}).Where("id IN ?", markUnreadIDs).Update("is_read", false).Error; err != nil {
				return err
			}
		}
		if len(starIDs) > 0 {
			if err := tx.Model(&models.Email{}).Where("id IN ?", starIDs).Update("is_starred", true).Error; err != nil {
				return err
			}
		}
		if len(unstarIDs) > 0 {
			if err := tx.Model(&models.Email{}).Where("id IN ?", unstarIDs).Update("is_starred", false).Error; err != nil {
				return err
			}
		}

		// Delete in SQL so pruning does not load historical IDs into memory.
		stale := tx.Unscoped().Model(&models.Email{}).Select("id").
			Where("account_id = ? AND folder = ?", account.ID, folder).
			Order("date DESC, id DESC").Limit(-1).Offset(limit)
		if err := tx.Unscoped().Where("id IN (?)", stale).Delete(&models.Email{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.EmailAccount{}).Where("id = ?", account.ID).
			Update("last_sync_time", syncTime).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to reconcile emails: %w", err)
	}
	account.LastSyncTime = syncTime

	// Only notify after the complete refresh has committed.
	if hadExisting && len(newEmails) > 0 {
		for i := range newEmails {
			telegram.SendNewEmailAsync(&newEmails[i], account.Email)
		}
	}
	return len(newEmails), nil
}

// RefreshAccounts refreshes multiple accounts concurrently and returns a combined summary.
func RefreshAccounts(ctx context.Context, db *gorm.DB, accounts []models.EmailAccount, limit int) RefreshSummary {
	summary := RefreshSummary{
		Errors: make(map[string]string),
	}
	if len(accounts) == 0 {
		return summary
	}

	workerCount := maxRefreshWorkers
	if len(accounts) < workerCount {
		workerCount = len(accounts)
	}

	jobs := make(chan models.EmailAccount, len(accounts))
	for _, acc := range accounts {
		jobs <- acc
	}
	close(jobs)

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for acc := range jobs {
				accCopy := acc
				newCount, err := RefreshAccount(ctx, db, &accCopy, limit)
				mu.Lock()
				if err != nil {
					summary.Errors[accCopy.Email] = err.Error()
				} else {
					summary.Refreshed++
					summary.NewCount += newCount
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(summary.Errors) == 0 {
		summary.Errors = nil
	}
	return summary
}
