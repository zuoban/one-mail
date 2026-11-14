package mail

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
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
	refreshAllTimeout = 35 * time.Second
	maxRefreshWorkers = 4
)

var ErrAccountChanged = errors.New("mailbox configuration changed; retry the request")

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

// Serialize refreshes for an account. Waiters are cancellable;
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

func RefreshAccount(ctx context.Context, db *gorm.DB, account *models.EmailAccount, limit int) (newCount int, resultErr error) {
	if account == nil {
		return 0, errors.New("account is nil")
	}
	started := time.Now()
	defer func() {
		log.Printf("mail_refresh account_id=%d duration_ms=%d new_count=%d success=%t", account.ID, time.Since(started).Milliseconds(), newCount, resultErr == nil)
	}()
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
	var current models.EmailAccount
	if err := db.WithContext(reqCtx).First(&current, account.ID).Error; err != nil {
		return 0, err
	}
	if current.ConfigVersion != account.ConfigVersion || current.IdentityVersion != account.IdentityVersion {
		return 0, ErrAccountChanged
	}
	*account = current
	client, release, err := imap.GetConnectionPool().AcquireConnectionContext(reqCtx, account)
	if err != nil {
		return 0, err
	}
	healthy := false
	defer func() { release(healthy) }()
	emails, epoch, err := client.FetchLatestEmails(reqCtx, DefaultFolder, limit)
	if err != nil {
		return 0, err
	}
	healthy = true
	return ReconcileLatestEmails(db.WithContext(reqCtx), account, DefaultFolder, epoch, emails, limit)
}

// ReconcileLatestEmails keys remote identity by (folder, UIDVALIDITY, UID).
// Message-ID is metadata and can legitimately be shared by separate messages.
func ReconcileLatestEmails(db *gorm.DB, account *models.EmailAccount, folder string, epoch uint32, summaries []*imap.EmailSummary, limit int) (int, error) {
	if account == nil {
		return 0, errors.New("account is nil")
	}
	if epoch == 0 {
		return 0, imap.ErrUIDValidityChanged
	}
	if limit <= 0 || limit > MaxLatestEmails {
		limit = MaxLatestEmails
	}
	if folder == "" {
		folder = DefaultFolder
	}
	latest := make([]*imap.EmailSummary, 0, len(summaries))
	seen := make(map[uint]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		if summary.UID == 0 {
			return 0, errors.New("message has no UID")
		}
		if _, duplicate := seen[summary.UID]; duplicate {
			continue
		}
		seen[summary.UID] = struct{}{}
		latest = append(latest, summary)
	}
	slices.SortFunc(latest, func(a, b *imap.EmailSummary) int {
		if a.Date.Before(b.Date) {
			return 1
		}
		if a.Date.After(b.Date) {
			return -1
		}
		if a.UID < b.UID {
			return 1
		}
		if a.UID > b.UID {
			return -1
		}
		return 0
	})
	if len(latest) > limit {
		latest = latest[:limit]
	}
	var newEmails []models.Email
	var hadExisting bool
	syncTime := time.Now()
	err := db.Transaction(func(tx *gorm.DB) error {
		var current models.EmailAccount
		if err := tx.First(&current, account.ID).Error; err != nil {
			return err
		}
		if current.ConfigVersion != account.ConfigVersion || current.IdentityVersion != account.IdentityVersion {
			return ErrAccountChanged
		}
		var existing []models.Email
		if err := tx.Select("id, uid, uid_validity, identity_version, message_id, `from`, from_name, `to`, subject, date, has_attachment").Where("account_id = ? AND folder = ?", account.ID, folder).
			Order("id DESC").Find(&existing).Error; err != nil {
			return err
		}
		hadExisting = len(existing) > 0
		byUID := make(map[uint]*models.Email, len(existing))
		for i := range existing {
			row := &existing[i]
			if row.UIDValidity == epoch && row.IdentityVersion == account.IdentityVersion {
				if _, duplicate := byUID[row.UID]; !duplicate {
					byUID[row.UID] = row
				}
			}
		}
		keepIDs := make([]uint, 0, len(latest))
		for _, summary := range latest {
			msgID := summary.MessageID
			if msgID == "" {
				msgID = fmt.Sprintf("uid-%d", summary.UID)
			}
			if row, exists := byUID[summary.UID]; exists {
				updates := summaryChanges(row, summary, msgID)
				if len(updates) > 0 {
					if err := tx.Model(&models.Email{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
						return err
					}
				}
				keepIDs = append(keepIDs, row.ID)
				continue
			}
			newEmails = append(newEmails, models.Email{
				AccountID: account.ID, MessageID: msgID, From: summary.From, FromName: summary.FromName,
				To: summary.To, Subject: summary.Subject, Date: summary.Date, HasAttachment: summary.HasAttachment,
				Folder: folder, UID: summary.UID, UIDValidity: epoch, IdentityVersion: account.IdentityVersion,
			})
		}
		if len(newEmails) > 0 {
			if err := tx.CreateInBatches(&newEmails, MaxLatestEmails).Error; err != nil {
				return err
			}
			for _, email := range newEmails {
				keepIDs = append(keepIDs, email.ID)
			}
		}
		// Remove obsolete epochs, identities, duplicate rows and their cached bodies together.
		stale := tx.Where("account_id = ? AND folder = ?", account.ID, folder)
		if len(keepIDs) > 0 {
			stale = stale.Where("id NOT IN ?", keepIDs)
		}
		if err := stale.Delete(&models.Email{}).Error; err != nil {
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
	if hadExisting {
		for i := range newEmails {
			telegram.SendNewEmailAsync(&newEmails[i], account.Email)
		}
	}
	return len(newEmails), nil
}
func summaryChanges(row *models.Email, summary *imap.EmailSummary, messageID string) map[string]interface{} {
	var updates map[string]interface{}
	set := func(key string, value interface{}) {
		if updates == nil {
			updates = make(map[string]interface{})
		}
		updates[key] = value
	}
	if row.MessageID != messageID {
		set("message_id", messageID)
	}
	if row.From != summary.From {
		set("from", summary.From)
	}
	if row.FromName != summary.FromName {
		set("from_name", summary.FromName)
	}
	if row.To != summary.To {
		set("to", summary.To)
	}
	if row.Subject != summary.Subject {
		set("subject", summary.Subject)
	}
	if !row.Date.Equal(summary.Date) {
		set("date", summary.Date)
	}
	if row.HasAttachment != summary.HasAttachment {
		set("has_attachment", summary.HasAttachment)
	}
	return updates
}

func RefreshAccounts(ctx context.Context, db *gorm.DB, accounts []models.EmailAccount, limit int) RefreshSummary {
	ctx, cancel := context.WithTimeout(ctx, refreshAllTimeout)
	defer cancel()
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
