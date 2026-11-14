package mail

import (
	"context"
	"errors"
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

type accountGate struct {
	token chan struct{}
	refs  int
}

var accountGates = struct {
	sync.Mutex
	items map[uint]*accountGate
}{items: make(map[uint]*accountGate)}

var refreshSlots = make(chan struct{}, maxRefreshWorkers)

var ErrPendingReadExpired = fmt.Errorf("%w: previous read changes expired; select messages again", imap.ErrUIDValidityChanged)

// Serialize refreshes and flag writes for an account. Waiters are cancellable;
// unused gates are removed instead of retaining one worker per account forever.
func lockAccount(ctx context.Context, id uint) (func(), error) {
	accountGates.Lock()
	gate := accountGates.items[id]
	if gate == nil {
		gate = &accountGate{token: make(chan struct{}, 1)}
		accountGates.items[id] = gate
	}
	gate.refs++
	accountGates.Unlock()
	drop := func() {
		accountGates.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(accountGates.items, id)
		}
		accountGates.Unlock()
	}
	select {
	case gate.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.token
			drop()
			return nil, err
		}
		return func() { <-gate.token; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

func RefreshAccount(ctx context.Context, db *gorm.DB, account *models.EmailAccount, limit int) (newCount int, refreshErr error) {
	if account == nil {
		return 0, errors.New("account is nil")
	}
	reqCtx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	unlock, err := lockAccount(reqCtx, account.ID)
	if err != nil {
		return 0, err
	}
	defer unlock()
	select {
	case refreshSlots <- struct{}{}:
		defer func() { <-refreshSlots }()
	case <-reqCtx.Done():
		return 0, reqCtx.Err()
	}
	client, release, err := imap.GetConnectionPool().AcquireConnectionContext(reqCtx, account)
	if err != nil {
		return 0, err
	}
	healthy := false
	defer func() { release(healthy) }()
	// Validate and migrate the epoch before applying any queued UID-based writes.
	epoch, err := client.SelectMailbox(reqCtx, DefaultFolder)
	if err != nil {
		return 0, err
	}
	var expiredPending bool
	if err := db.WithContext(reqCtx).Transaction(func(tx *gorm.DB) error {
		var err error
		expiredPending, err = prepareEpoch(tx, account.ID, DefaultFolder, epoch)
		return err
	}); err != nil {
		return 0, err
	}
	if expiredPending {
		defer func() { refreshErr = errors.Join(ErrPendingReadExpired, refreshErr) }()
	}
	pendingErr := syncPendingRead(reqCtx, db, account, client.SetSeen)
	if pendingErr != nil && !errors.Is(pendingErr, imap.ErrUIDValidityChanged) {
		return 0, pendingErr
	}
	emails, fetchedEpoch, err := client.FetchLatestEmails(reqCtx, DefaultFolder, limit)
	if err != nil {
		return 0, err
	}
	if fetchedEpoch != epoch {
		return 0, imap.ErrUIDValidityChanged
	}
	healthy = true
	count, err := ReconcileLatestEmails(db.WithContext(reqCtx), account, DefaultFolder, epoch, emails, limit)
	if err != nil {
		return 0, err
	}
	return count, pendingErr
}

// prepareEpoch runs in the same transaction as reconciliation, and refuses
// resurrection after an account deletion. A UID is meaningful only in its epoch.
func prepareEpoch(tx *gorm.DB, accountID uint, folder string, epoch uint32) (bool, error) {
	if epoch == 0 {
		return false, imap.ErrUIDValidityChanged
	}
	var current models.EmailAccount
	if err := tx.First(&current, accountID).Error; err != nil {
		return false, err
	}
	var pending int64
	if err := tx.Unscoped().Model(&models.Email{}).
		Where("account_id = ? AND folder = ? AND uid_validity <> ? AND pending_read IS NOT NULL", accountID, folder, epoch).
		Count(&pending).Error; err != nil {
		return false, err
	}
	// These identities no longer exist. Drop their bodies and intents together;
	// the caller reports lost intent once after the new snapshot commits.
	if err := tx.Unscoped().Where("account_id = ? AND folder = ? AND uid_validity <> ?", accountID, folder, epoch).
		Delete(&models.Email{}).Error; err != nil {
		return false, err
	}
	if folder == DefaultFolder && current.UIDValidity != epoch {
		if err := tx.Model(&models.EmailAccount{}).Where("id = ?", accountID).Update("uid_validity", epoch).Error; err != nil {
			return false, err
		}
	}
	return pending > 0, nil
}

// SyncPendingRead makes one bounded realtime attempt. Failures leave the durable
// intent intact, including unread=false; callers can report pending state.
func SyncPendingRead(ctx context.Context, db *gorm.DB, account *models.EmailAccount) error {
	if account == nil {
		return errors.New("account is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	unlock, err := lockAccount(ctx, account.ID)
	if err != nil {
		return err
	}
	defer unlock()
	var count int64
	if err := db.WithContext(ctx).Model(&models.Email{}).Where("account_id = ? AND pending_read IS NOT NULL", account.ID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	client, release, err := imap.GetConnectionPool().AcquireConnectionContext(ctx, account)
	if err != nil {
		return err
	}
	err = syncPendingRead(ctx, db, account, client.SetSeen)
	release(err == nil)
	return err
}

func syncPendingRead(ctx context.Context, db *gorm.DB, account *models.EmailAccount, setSeen func(context.Context, uint, string, uint32, bool) error) error {
	var current models.EmailAccount
	if err := db.WithContext(ctx).First(&current, account.ID).Error; err != nil {
		return err
	}
	var pending []models.Email
	if err := db.WithContext(ctx).Where("account_id = ? AND pending_read IS NOT NULL", account.ID).Find(&pending).Error; err != nil {
		return err
	}
	var pendingErr error
	for _, email := range pending {
		if email.UIDValidity == 0 || email.UID == 0 || (email.Folder == DefaultFolder && email.UIDValidity != current.UIDValidity) {
			pendingErr = imap.ErrUIDValidityChanged
			continue
		}
		desired := *email.PendingRead
		if err := setSeen(ctx, email.UID, email.Folder, email.UIDValidity, desired); err != nil {
			if errors.Is(err, imap.ErrUIDValidityChanged) {
				pendingErr = err
				continue
			}
			return err
		}
		if err := clearPendingRead(db.WithContext(ctx), email, desired); err != nil {
			return err
		}
	}
	return pendingErr
}

func clearPendingRead(db *gorm.DB, email models.Email, desired bool) error {
	// Compare updated_at as well as the desired value so true→false→true during
	// STORE is retained as a new intent instead of being cleared by an older write.
	return db.Model(&models.Email{}).
		Where("id = ? AND uid = ? AND uid_validity = ? AND pending_read = ? AND updated_at = ?", email.ID, email.UID, email.UIDValidity, desired, email.UpdatedAt).
		Update("pending_read", nil).Error
}

// ReconcileLatestEmails keys remote identity by (folder, UIDVALIDITY, UID).
// Message-ID is metadata and can legitimately be shared by separate messages.
func ReconcileLatestEmails(db *gorm.DB, account *models.EmailAccount, folder string, epoch uint32, summaries []*imap.EmailSummary, limit int) (int, error) {
	if account == nil {
		return 0, errors.New("account is nil")
	}
	if limit <= 0 || limit > MaxLatestEmails {
		limit = MaxLatestEmails
	}
	if folder == "" {
		folder = DefaultFolder
	}
	for _, summary := range summaries {
		if summary != nil && summary.UID == 0 {
			return 0, errors.New("message has no UID")
		}
	}
	var newEmails []models.Email
	var hadExisting bool
	var expiredPending bool
	syncTime := time.Now()
	err := db.Transaction(func(tx *gorm.DB) error {
		var prior int64
		if err := tx.Unscoped().Model(&models.Email{}).Where("account_id = ? AND folder = ?", account.ID, folder).Count(&prior).Error; err != nil {
			return err
		}
		hadExisting = prior > 0
		var err error
		expiredPending, err = prepareEpoch(tx, account.ID, folder, epoch)
		if err != nil {
			return err
		}
		var existing []models.Email
		if err := tx.Unscoped().Where("account_id = ? AND folder = ? AND uid_validity = ?", account.ID, folder, epoch).Find(&existing).Error; err != nil {
			return err
		}
		byUID := make(map[uint]models.Email, len(existing))
		for _, row := range existing {
			byUID[row.UID] = row
		}
		seen := make(map[uint]struct{}, len(summaries))
		for _, summary := range summaries {
			if summary == nil {
				continue
			}
			if _, duplicate := seen[summary.UID]; duplicate {
				continue
			}
			seen[summary.UID] = struct{}{}
			msgID := summary.MessageID
			if msgID == "" {
				msgID = fmt.Sprintf("uid-%d", summary.UID)
			}
			if row, exists := byUID[summary.UID]; exists {
				if row.DeletedAt.Valid {
					continue
				}
				if err := tx.Model(&models.Email{}).Where("id = ?", row.ID).Updates(map[string]interface{}{
					"message_id": msgID, "is_read": gorm.Expr("CASE WHEN pending_read IS NOT NULL THEN pending_read ELSE ? END", summary.IsRead),
					"is_starred": summary.IsStarred,
				}).Error; err != nil {
					return err
				}
				continue
			}
			newEmails = append(newEmails, models.Email{
				AccountID: account.ID, MessageID: msgID, From: summary.From, FromName: summary.FromName,
				To: summary.To, Subject: summary.Subject, Date: summary.Date, HasAttachment: summary.HasAttachment,
				IsRead: summary.IsRead, IsStarred: summary.IsStarred, Folder: folder, UID: summary.UID, UIDValidity: epoch,
			})
		}
		if len(newEmails) > 0 {
			if err := tx.CreateInBatches(&newEmails, 50).Error; err != nil {
				return err
			}
		}
		// A moved UID never inherits the old body's cache or local deletion marker.
		// Keep pending intents outside the latest window until they are written back.
		uids := make([]uint, 0, len(seen))
		for uid := range seen {
			uids = append(uids, uid)
		}
		stale := tx.Unscoped().Where("account_id = ? AND folder = ? AND pending_read IS NULL", account.ID, folder)
		if len(uids) > 0 {
			stale = stale.Where("uid NOT IN ?", uids)
		}
		if err := stale.Delete(&models.Email{}).Error; err != nil {
			return err
		}
		older := tx.Unscoped().Model(&models.Email{}).Select("id").Where("account_id = ? AND folder = ? AND pending_read IS NULL", account.ID, folder).
			Order("date DESC, id DESC").Limit(-1).Offset(limit)
		if err := tx.Unscoped().Where("id IN (?)", older).Delete(&models.Email{}).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{"last_sync_time": syncTime}
		if folder == DefaultFolder {
			updates["uid_validity"] = epoch
		}
		return tx.Model(&models.EmailAccount{}).Where("id = ?", account.ID).Updates(updates).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to reconcile emails: %w", err)
	}
	account.LastSyncTime = syncTime
	if folder == DefaultFolder {
		account.UIDValidity = epoch
	}
	if hadExisting && len(newEmails) > 0 {
		for i := range newEmails {
			telegram.SendNewEmailAsync(&newEmails[i], account.Email)
		}
	}
	if expiredPending {
		return len(newEmails), ErrPendingReadExpired
	}
	return len(newEmails), nil
}

func RefreshAccounts(ctx context.Context, db *gorm.DB, accounts []models.EmailAccount, limit int) RefreshSummary {
	summary := RefreshSummary{Errors: make(map[string]string)}
	jobs := make(chan models.EmailAccount, len(accounts))
	for _, account := range accounts {
		jobs <- account
	}
	close(jobs)
	workers := min(maxRefreshWorkers, len(accounts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for account := range jobs {
				count, err := RefreshAccount(ctx, db, &account, limit)
				mu.Lock()
				if err != nil {
					summary.Errors[account.Email] = err.Error()
				} else {
					summary.Refreshed++
					summary.NewCount += count
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
