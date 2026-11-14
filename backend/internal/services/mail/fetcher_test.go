package mail

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

func setupFetcherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fetcher_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetupSQLite(db, true); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateAndOptimize(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReconcileLatestEmails_CapsAt50UpdatesFlagsAndRespectsDeletes(t *testing.T) {
	db := setupFetcherTestDB(t)

	acc := models.EmailAccount{
		Email: "u@example.com", Provider: "custom",
		IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587,
		Username: "u", Password: "p",
	}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// Initial seed of 50 emails
	initial := make([]*imap.EmailSummary, 0, 50)
	for i := 1; i <= 50; i++ {
		initial = append(initial, &imap.EmailSummary{
			MessageID: fmt.Sprintf("msg-%d", i),
			From:      "sender@example.com",
			To:        acc.Email,
			Subject:   fmt.Sprintf("Subject %d", i),
			Date:      base.Add(time.Duration(i) * time.Minute),
			UID:       uint(i),
			IsRead:    false,
		})
	}

	inserted, err := ReconcileLatestEmails(db, &acc, "INBOX", 1, initial, 50)
	if err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}
	if inserted != 50 {
		t.Fatalf("expected 50 inserted, got %d", inserted)
	}

	// Soft-delete msg-49 locally; should not be resurrected on next refresh
	if err := db.Where("account_id = ? AND message_id = ?", acc.ID, "msg-49").Delete(&models.Email{}).Error; err != nil {
		t.Fatal(err)
	}

	// Next refresh brings 3 newer emails (51..53) + marks msg-50 as read, while sliding the 50-email window to 4..53
	nextBatch := make([]*imap.EmailSummary, 0, 50)
	for i := 4; i <= 53; i++ {
		nextBatch = append(nextBatch, &imap.EmailSummary{
			MessageID: fmt.Sprintf("msg-%d", i),
			From:      "sender@example.com",
			To:        acc.Email,
			Subject:   fmt.Sprintf("Subject %d", i),
			Date:      base.Add(time.Duration(i) * time.Minute),
			UID:       uint(i),
			IsRead:    i == 50,
		})
	}

	inserted2, err := ReconcileLatestEmails(db, &acc, "INBOX", 1, nextBatch, 50)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	if inserted2 != 3 {
		t.Fatalf("expected 3 new emails inserted, got %d", inserted2)
	}

	var totalUnscoped int64
	db.Unscoped().Model(&models.Email{}).Where("account_id = ?", acc.ID).Count(&totalUnscoped)
	if totalUnscoped != 50 {
		t.Fatalf("expected total rows pruned to 50, got %d", totalUnscoped)
	}

	// msg-49 stays soft-deleted
	var activeCount int64
	db.Model(&models.Email{}).Where("account_id = ?", acc.ID).Count(&activeCount)
	if activeCount != 49 {
		t.Fatalf("expected 49 active emails (with msg-49 soft-deleted), got %d", activeCount)
	}

	// msg-50 flag updated to read
	var msg50 models.Email
	if err := db.Where("account_id = ? AND message_id = ?", acc.ID, "msg-50").First(&msg50).Error; err != nil {
		t.Fatal(err)
	}
	if !msg50.IsRead {
		t.Fatal("expected msg-50 IsRead to be updated to true")
	}

	// Oldest msg-1..msg-3 pruned
	var oldestCount int64
	db.Unscoped().Model(&models.Email{}).Where("account_id = ? AND message_id IN ?", acc.ID, []string{"msg-1", "msg-2", "msg-3"}).Count(&oldestCount)
	if oldestCount != 0 {
		t.Fatalf("expected msg-1..3 to be pruned, found %d", oldestCount)
	}
}

func TestReconcileLatestEmails_RollsBackFailedRefresh(t *testing.T) {
	for _, stage := range []struct {
		name    string
		trigger string
	}{
		{"flags", "BEFORE UPDATE OF is_read ON emails"},
		{"pruning", "BEFORE DELETE ON emails"},
		{"sync_time", "BEFORE UPDATE OF last_sync_time ON email_accounts"},
	} {
		t.Run(stage.name, func(t *testing.T) {
			db := setupFetcherTestDB(t)
			previousSync := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
			account := models.EmailAccount{Email: "rollback@example.com", LastSyncTime: previousSync}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			original := models.Email{
				AccountID: account.ID, MessageID: "original", Folder: DefaultFolder,
				UID: 1, UIDValidity: 1, Date: previousSync, BodyText: "cached body",
			}
			if err := db.Create(&original).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TRIGGER fail_refresh " + stage.trigger +
				" BEGIN SELECT RAISE(FAIL, 'simulated refresh failure'); END").Error; err != nil {
				t.Fatal(err)
			}

			inserted, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, []*imap.EmailSummary{
				{MessageID: "original", UID: 1, Date: previousSync, IsRead: true, IsStarred: true},
				{MessageID: "new", UID: 2, Date: previousSync.Add(time.Minute)},
			}, 1)
			if err == nil || inserted != 0 {
				t.Fatalf("failed refresh must report an error and no inserts, got count=%d err=%v", inserted, err)
			}
			var stored []models.Email
			if err := db.Unscoped().Where("account_id = ?", account.ID).Find(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if len(stored) != 1 || stored[0].ID != original.ID || stored[0].IsRead || stored[0].IsStarred || stored[0].BodyText != "cached body" {
				t.Fatalf("failed refresh changed stored mail: %+v", stored)
			}
			var storedAccount models.EmailAccount
			if err := db.First(&storedAccount, account.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !storedAccount.LastSyncTime.Equal(previousSync) || !account.LastSyncTime.Equal(previousSync) {
				t.Fatalf("failed refresh advanced sync time: stored=%v account=%v", storedAccount.LastSyncTime, account.LastSyncTime)
			}
		})
	}
}

func TestReconcileLatestEmails_MatchesUIDAndMissingMessageID(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "matching@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	original := models.Email{
		AccountID: account.ID, MessageID: "previous-id", Folder: DefaultFolder,
		UID: 7, UIDValidity: 1, BodyText: "keep cached body",
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{
		nil,
		{MessageID: "changed-id", UID: 7, IsRead: true, IsStarred: true},
		{UID: 8, Subject: "missing message id"},
		{UID: 8, Subject: "duplicate must be ignored"},
	}
	inserted, err := ReconcileLatestEmails(db, &account, "", 1, batch, 50)
	if err != nil || inserted != 1 {
		t.Fatalf("expected one new email, got count=%d err=%v", inserted, err)
	}
	inserted, err = ReconcileLatestEmails(db, &account, "", 1, batch, 50)
	if err != nil || inserted != 0 {
		t.Fatalf("repeated refresh must not duplicate mail, got count=%d err=%v", inserted, err)
	}
	var stored []models.Email
	if err := db.Where("account_id = ?", account.ID).Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].ID != original.ID || !stored[0].IsRead || !stored[0].IsStarred || stored[0].BodyText != "keep cached body" {
		t.Fatalf("UID matching failed to preserve cached email: %+v", stored)
	}
	if stored[1].MessageID != "uid-8" || stored[1].Subject != "missing message id" {
		t.Fatalf("missing Message-ID was not deduplicated: %+v", stored[1])
	}
}

func TestReconcileLatestEmails_EpochAndUIDIdentity(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "epoch@example.com", UIDValidity: 10}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	old := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "same", UID: 1, UIDValidity: 10, BodyText: "old secret", BodyFetched: true}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{MessageID: "same", UID: 1}, {MessageID: "same", UID: 2}}
	count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 11, batch, 50)
	if err != nil || count != 2 {
		t.Fatalf("epoch reset count=%d err=%v", count, err)
	}
	var stored []models.Email
	if err := db.Unscoped().Order("uid").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("same Message-ID collapsed distinct UIDs: %+v", stored)
	}
	for _, email := range stored {
		if email.ID == old.ID || email.UIDValidity != 11 || email.BodyText != "" || email.BodyFetched {
			t.Fatalf("old epoch cache leaked: %+v", email)
		}
	}
	if account.UIDValidity != 11 {
		t.Fatal("account epoch was not persisted")
	}
	// The message at a new UID is a new remote identity even when Message-ID repeats.
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 11, []*imap.EmailSummary{{MessageID: "same", UID: 3}}, 50); err != nil {
		t.Fatal(err)
	}
	stored = nil
	if err := db.Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].UID != 3 || stored[0].BodyFetched {
		t.Fatalf("moved UID inherited cache: %+v", stored)
	}
	// An empty mailbox still carries its epoch and clears the prior folder cache.
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 12, nil, 50); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	db.Unscoped().Model(&models.Email{}).Count(&remaining)
	if remaining != 0 || account.UIDValidity != 12 {
		t.Fatalf("empty epoch not reconciled: rows=%d epoch=%d", remaining, account.UIDValidity)
	}
}

func TestReconcileLatestEmails_PreservesPendingReadAndConditionalClear(t *testing.T) {
	for _, desired := range []bool{false, true} {
		t.Run(fmt.Sprint(desired), func(t *testing.T) {
			db := setupFetcherTestDB(t)
			account := models.EmailAccount{Email: "pending@example.com", UIDValidity: 1}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "pending", UID: 1, UIDValidity: 1, IsRead: desired, PendingRead: &desired}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, []*imap.EmailSummary{{UID: 1, IsRead: !desired}}, 50); err != nil {
				t.Fatal(err)
			}
			var current models.Email
			if err := db.First(&current, email.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.PendingRead == nil || *current.PendingRead != desired || current.IsRead != desired {
				t.Fatalf("refresh overwrote pending: %+v", current)
			}
			// A newer write of the same desired state must survive the earlier acknowledgement.
			if err := db.Model(&current).Updates(map[string]interface{}{"pending_read": desired, "updated_at": current.UpdatedAt.Add(time.Second)}).Error; err != nil {
				t.Fatal(err)
			}
			if err := clearPendingRead(db, email, desired); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&current, email.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.PendingRead == nil {
				t.Fatal("older write cleared newer intent")
			}
			if err := clearPendingRead(db, current, desired); err != nil {
				t.Fatal(err)
			}
			var cleared models.Email
			if err := db.First(&cleared, email.ID).Error; err != nil {
				t.Fatal(err)
			}
			if cleared.PendingRead != nil {
				t.Fatal("matching acknowledgement did not clear pending")
			}
		})
	}
}

func TestReconcileLatestEmails_RejectsDeletedAccountAndUnknownEpoch(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "deleted@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 0, nil, 50); !errors.Is(err, imap.ErrUIDValidityChanged) {
		t.Fatalf("unknown epoch accepted: %v", err)
	}
	if err := db.Delete(&account).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, []*imap.EmailSummary{{UID: 1}}, 50); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted account resurrected: %v", err)
	}
	var count int64
	db.Unscoped().Model(&models.Email{}).Count(&count)
	if count != 0 {
		t.Fatalf("deleted account received %d emails", count)
	}
}

func TestAccountSerialization_CancelsWaitersAndReleasesGate(t *testing.T) {
	unlock, err := lockAccount(context.Background(), 777)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lockAccount(ctx, 777); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait not cancelled: %v", err)
	}
	unlock()
	accountGates.Lock()
	_, exists := accountGates.items[777]
	accountGates.Unlock()
	if exists {
		t.Fatal("completed account gate leaked")
	}
}

func TestSyncPendingRead_FailureRetainsIntentAndSuccessfulRetryClears(t *testing.T) {
	for _, desired := range []bool{false, true} {
		t.Run(fmt.Sprint(desired), func(t *testing.T) {
			db := setupFetcherTestDB(t)
			account := models.EmailAccount{Email: "sync@example.com", UIDValidity: 1}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "intent", UID: 7, UIDValidity: 1, IsRead: desired, PendingRead: &desired}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			remoteErr := errors.New("remote unavailable")
			if err := syncPendingRead(context.Background(), db, &account, func(context.Context, uint, string, uint32, bool) error { return remoteErr }); !errors.Is(err, remoteErr) {
				t.Fatalf("remote failure hidden: %v", err)
			}
			var stored models.Email
			if err := db.First(&stored, email.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.PendingRead == nil || *stored.PendingRead != desired || stored.IsRead != desired {
				t.Fatalf("failed write discarded intent: %+v", stored)
			}
			if err := syncPendingRead(context.Background(), db, &account, func(_ context.Context, uid uint, folder string, epoch uint32, seen bool) error {
				if uid != 7 || folder != DefaultFolder || epoch != 1 || seen != desired {
					t.Fatalf("wrong remote identity/flag: %d %s %d %v", uid, folder, epoch, seen)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var cleared models.Email
			if err := db.First(&cleared, email.ID).Error; err != nil {
				t.Fatal(err)
			}
			if cleared.PendingRead != nil || cleared.IsRead != desired {
				t.Fatalf("successful write did not clear matching intent: %+v", cleared)
			}
		})
	}
}

func TestReconcileLatestEmails_ExpiredPendingConflictsOnce(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "expired@example.com", UIDValidity: 1}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	desired := true
	old := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "reused", UID: 7, UIDValidity: 1, PendingRead: &desired, BodyFetched: true, BodyText: "old body"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{UID: 7, MessageID: "reused"}}
	count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 2, batch, 50)
	if count != 1 || !errors.Is(err, ErrPendingReadExpired) {
		t.Fatalf("lost intent was not reported: count=%d err=%v", count, err)
	}
	var stored []models.Email
	if err := db.Unscoped().Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID == old.ID || stored[0].PendingRead != nil || stored[0].BodyFetched || stored[0].BodyText != "" {
		t.Fatalf("expired intent/cache survived: %+v", stored)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 2, batch, 50); count != 0 || err != nil {
		t.Fatalf("expired intent caused permanent failure: count=%d err=%v", count, err)
	}
}

func TestSyncPendingRead_UnknownEpochNeverWritesUID(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "unknown@example.com", UIDValidity: 1}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	desired := true
	email := models.Email{AccountID: account.ID, Folder: DefaultFolder, UID: 7, PendingRead: &desired}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	err := syncPendingRead(context.Background(), db, &account, func(context.Context, uint, string, uint32, bool) error {
		t.Fatal("unknown epoch wrote a UID")
		return nil
	})
	if !errors.Is(err, imap.ErrUIDValidityChanged) {
		t.Fatalf("unknown epoch accepted: %v", err)
	}
}
