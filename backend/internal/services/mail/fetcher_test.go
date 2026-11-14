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

func TestReconcileLatestEmails_Default50AndEvictsBodies(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "retention@example.com"}
	other := models.EmailAccount{Email: "other@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	isolated := []models.Email{
		{AccountID: account.ID, Folder: "Archive", MessageID: "archive", UID: 1, UIDValidity: 1, BodyFetched: true, BodyText: "archive body"},
		{AccountID: other.ID, Folder: DefaultFolder, MessageID: "other", UID: 1, UIDValidity: 1, BodyFetched: true, BodyText: "other body"},
	}
	if err := db.Create(&isolated).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	batch := func(first, last int) []*imap.EmailSummary {
		var summaries []*imap.EmailSummary
		for i := first; i <= last; i++ {
			summaries = append(summaries, &imap.EmailSummary{
				MessageID: fmt.Sprintf("msg-%d", i), UID: uint(i), Subject: fmt.Sprintf("Subject %d", i),
				From: "sender@example.com", To: account.Email, Date: base.Add(time.Duration(i) * time.Minute),
			})
		}
		return summaries
	}
	if inserted, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, batch(1, 60), 0); err != nil || inserted != 50 {
		t.Fatalf("default refresh count=%d err=%v", inserted, err)
	}
	var evicted, retained models.Email
	if err := db.Where("account_id = ? AND folder = ? AND uid = ?", account.ID, DefaultFolder, 11).First(&evicted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("account_id = ? AND folder = ? AND uid = ?", account.ID, DefaultFolder, 60).First(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Email{}).Where("id IN ?", []uint{evicted.ID, retained.ID}).
		Updates(map[string]interface{}{"body_text": "cached body", "body_fetched": true}).Error; err != nil {
		t.Fatal(err)
	}
	if inserted, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, batch(14, 63), 50); err != nil || inserted != 3 {
		t.Fatalf("sliding window count=%d err=%v", inserted, err)
	}
	var stored []models.Email
	if err := db.Unscoped().Where("account_id = ? AND folder = ?", account.ID, DefaultFolder).Order("uid").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 50 || stored[0].UID != 14 || stored[49].UID != 63 {
		t.Fatalf("unexpected retained window: %+v", stored)
	}
	for _, email := range stored {
		if email.ID == evicted.ID {
			t.Fatal("evicted body retained a physical row")
		}
		if email.UID == 60 && (email.ID != retained.ID || email.BodyText != "cached body" || !email.BodyFetched) {
			t.Fatalf("retained identity lost its body: %+v", email)
		}
	}
	for _, original := range isolated {
		var email models.Email
		if err := db.First(&email, original.ID).Error; err != nil || email.BodyText != original.BodyText {
			t.Fatalf("refresh affected another account/folder: %+v %v", email, err)
		}
	}
}

func TestReconcileLatestEmails_RollsBackFailedRefresh(t *testing.T) {
	for _, stage := range []struct {
		name    string
		trigger string
		limit   int
	}{
		{"metadata", "BEFORE UPDATE OF subject ON emails", 2},
		{"pruning", "BEFORE DELETE ON emails", 1},
		{"sync_time", "BEFORE UPDATE OF last_sync_time ON email_accounts", 1},
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
				{MessageID: "original", UID: 1, Date: previousSync, Subject: "updated"},
				{MessageID: "new", UID: 2, Date: previousSync.Add(time.Minute)},
			}, stage.limit)
			if err == nil || inserted != 0 {
				t.Fatalf("failed refresh must report an error and no inserts, got count=%d err=%v", inserted, err)
			}
			var stored []models.Email
			if err := db.Unscoped().Where("account_id = ?", account.ID).Find(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if len(stored) != 1 || stored[0].ID != original.ID || stored[0].Subject != "" || stored[0].BodyText != "cached body" {
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
		UID: 7, UIDValidity: 1, BodyText: "keep cached body", BodyFetched: true,
	}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{
		nil,
		{MessageID: "changed-id", UID: 7, From: "updated@example.com", FromName: "Updated", To: "recipient@example.com", Subject: "updated subject", Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), HasAttachment: true},
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
	if len(stored) != 2 || stored[0].ID != original.ID || stored[0].MessageID != "changed-id" || stored[0].From != batch[1].From || stored[0].FromName != batch[1].FromName || stored[0].To != batch[1].To || stored[0].Subject != batch[1].Subject || !stored[0].Date.Equal(batch[1].Date) || !stored[0].HasAttachment || stored[0].BodyText != "keep cached body" || !stored[0].BodyFetched {
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

func TestReconcileLatestEmails_FailedEpochChangePreservesCache(t *testing.T) {
	db := setupFetcherTestDB(t)
	previousSync := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	account := models.EmailAccount{Email: "epoch-rollback@example.com", UIDValidity: 10, LastSyncTime: previousSync}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	original := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "original", UID: 7, UIDValidity: 10, BodyText: "cached private body", BodyFetched: true}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_epoch BEFORE UPDATE OF uid_validity ON email_accounts BEGIN SELECT RAISE(FAIL, 'epoch commit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 11, []*imap.EmailSummary{{UID: 7}}, 50); count != 0 || err == nil {
		t.Fatalf("failed epoch reconciliation count=%d err=%v", count, err)
	}
	var stored []models.Email
	if err := db.Unscoped().Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != original.ID || stored[0].UIDValidity != 10 || stored[0].BodyText != original.BodyText || !stored[0].BodyFetched {
		t.Fatalf("failed epoch change destroyed cache: %+v", stored)
	}
	var current models.EmailAccount
	if err := db.First(&current, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.UIDValidity != 10 || account.UIDValidity != 10 || !current.LastSyncTime.Equal(previousSync) || !account.LastSyncTime.Equal(previousSync) {
		t.Fatalf("failed epoch change advanced account: %+v %+v", current, account)
	}
}

func TestRefreshAccount_RemoteFailurePreservesCache(t *testing.T) {
	db := setupFetcherTestDB(t)
	previousSync := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	account := models.EmailAccount{Email: "unavailable@example.com", IMAPHost: "127.0.0.1", IMAPPort: 1, UIDValidity: 10, LastSyncTime: previousSync}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	original := models.Email{AccountID: account.ID, Folder: DefaultFolder, MessageID: "original", UID: 7, UIDValidity: 10, BodyText: "cached body", BodyFetched: true}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	if count, err := RefreshAccount(ctx, db, pool, &account, 50); count != 0 || err == nil {
		t.Fatalf("remote failure count=%d err=%v", count, err)
	}
	var stored []models.Email
	if err := db.Unscoped().Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != original.ID || stored[0].BodyText != original.BodyText || !stored[0].BodyFetched || stored[0].UIDValidity != 10 {
		t.Fatalf("remote failure changed cache: %+v", stored)
	}
	var current models.EmailAccount
	if err := db.First(&current, account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.UIDValidity != 10 || account.UIDValidity != 10 || !current.LastSyncTime.Equal(previousSync) || !account.LastSyncTime.Equal(previousSync) {
		t.Fatalf("remote failure advanced account: %+v %+v", current, account)
	}
}

func TestReconcileLatestEmails_ExpandedWindowPreservesCachedBodies(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "expanded@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	batch := func(first, last int) []*imap.EmailSummary {
		var result []*imap.EmailSummary
		for uid := first; uid <= last; uid++ {
			result = append(result, &imap.EmailSummary{UID: uint(uid), Date: base.Add(time.Duration(uid) * time.Minute)})
		}
		return result
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(101, 200), 100); err != nil || count != 100 {
		t.Fatalf("100-message refresh: count=%d err=%v", count, err)
	}
	var cached models.Email
	if err := db.Where("account_id = ? AND uid = ?", account.ID, 101).First(&cached).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&cached).Updates(map[string]interface{}{"body_fetched": true, "body_text": "cached plain", "body_html": "<p>cached</p>"}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(1, 200), 200); err != nil || count != 100 {
		t.Fatalf("200-message backfill: count=%d err=%v", count, err)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(2, 201), 200); err != nil || count != 1 {
		t.Fatalf("200-message sliding refresh: count=%d err=%v", count, err)
	}
	var stored []models.Email
	if err := db.Where("account_id = ?", account.ID).Order("uid").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 200 || stored[0].UID != 2 || stored[199].UID != 201 {
		t.Fatalf("configured window lost: count=%d", len(stored))
	}
	var retained models.Email
	if err := db.First(&retained, cached.ID).Error; err != nil || retained.UID != 101 || !retained.BodyFetched || retained.BodyText != "cached plain" || retained.BodyHTML != "<p>cached</p>" {
		t.Fatalf("matching cached identity/body lost: %+v err=%v", retained, err)
	}
}
