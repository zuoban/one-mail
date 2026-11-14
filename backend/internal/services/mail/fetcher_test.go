package mail

import (
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

	u := models.User{Username: "u", Password: "p", Email: "u@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	acc := models.EmailAccount{
		UserID: u.ID, Email: "u@example.com", Provider: "custom",
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

	inserted, err := ReconcileLatestEmails(db, &acc, "INBOX", initial, 50)
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

	inserted2, err := ReconcileLatestEmails(db, &acc, "INBOX", nextBatch, 50)
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
				UID: 1, Date: previousSync, BodyText: "cached body",
			}
			if err := db.Create(&original).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("CREATE TRIGGER fail_refresh " + stage.trigger +
				" BEGIN SELECT RAISE(FAIL, 'simulated refresh failure'); END").Error; err != nil {
				t.Fatal(err)
			}

			inserted, err := ReconcileLatestEmails(db, &account, DefaultFolder, []*imap.EmailSummary{
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
		UID: 7, BodyText: "keep cached body",
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
	inserted, err := ReconcileLatestEmails(db, &account, "", batch, 50)
	if err != nil || inserted != 1 {
		t.Fatalf("expected one new email, got count=%d err=%v", inserted, err)
	}
	inserted, err = ReconcileLatestEmails(db, &account, "", batch, 50)
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
