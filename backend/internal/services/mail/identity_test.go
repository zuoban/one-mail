package mail

import (
	"context"
	"errors"
	"testing"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

func TestReconcileRejectsOldConfigurationAndSeparatesMailboxIdentity(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "old@example.com", UIDValidity: 7}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	old := models.Email{AccountID: account.ID, Folder: DefaultFolder, UID: 1, UIDValidity: 7, MessageID: "old", BodyFetched: true, BodyText: "old mailbox body"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	stale := account
	if err := db.Model(&account).Updates(map[string]interface{}{"identity_version": 1, "config_version": 1, "email": "new@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{UID: 1, Subject: "new mailbox subject"}}
	if _, err := ReconcileLatestEmails(db, &stale, DefaultFolder, 7, batch, 50); !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("stale configuration committed: %v", err)
	}
	var retained models.Email
	if err := db.First(&retained, old.ID).Error; err != nil || retained.Subject != "" || retained.BodyText != old.BodyText {
		t.Fatalf("rejected refresh altered cache: %+v %v", retained, err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch, 50); err != nil {
		t.Fatal(err)
	}
	var messages []models.Email
	if err := db.Where("account_id = ?", account.ID).Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID == old.ID || messages[0].IdentityVersion != 1 || messages[0].Subject != batch[0].Subject || messages[0].BodyFetched || messages[0].BodyText != "" {
		t.Fatalf("different mailbox inherited old body: %+v", messages)
	}
}

func TestReconcilePasswordRotationRejectsOldTaskAndRetainsBody(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{UIDValidity: 8}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	old := models.Email{AccountID: account.ID, Folder: DefaultFolder, UID: 1, UIDValidity: 8, MessageID: "same", BodyFetched: true, BodyText: "valid cached body"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	stale := account
	if err := db.Model(&account).Update("config_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{UID: 1, MessageID: "same", Subject: "updated"}}
	if _, err := ReconcileLatestEmails(db, &stale, DefaultFolder, 8, batch, 50); !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("old credentials accepted: %v", err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 8, batch, 50); err != nil {
		t.Fatal(err)
	}
	var stored models.Email
	if err := db.First(&stored, old.ID).Error; err != nil || stored.BodyText != old.BodyText || !stored.BodyFetched || stored.Subject != "updated" {
		t.Fatalf("password rotation lost valid body: %+v %v", stored, err)
	}
}

func TestUnchangedRefreshDoesNotRewriteMail(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{UID: 1, MessageID: "same", Subject: "unchanged", Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)}}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, batch, 50); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_redundant_write BEFORE UPDATE ON emails BEGIN SELECT RAISE(FAIL, 'redundant write'); END").Error; err != nil {
		t.Fatal(err)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, batch, 50); err != nil || count != 0 {
		t.Fatalf("unchanged refresh rewrote metadata: count=%d err=%v", count, err)
	}
	batch[0].Subject = "changed"
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 1, batch, 50); err == nil {
		t.Fatal("changed metadata was not written")
	}
}

func TestRefreshRejectsStaleSnapshotBeforeConnecting(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{IMAPHost: "127.0.0.1", IMAPPort: 1}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.EmailAccount{}).Where("id = ?", account.ID).Update("config_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	if _, err := RefreshAccount(context.Background(), db, pool, &account, 50); !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("stale snapshot reached network: %v", err)
	}
}

func TestRefreshAccountsCancellationCoversQueuedAccounts(t *testing.T) {
	db := setupFetcherTestDB(t)
	accounts := make([]models.EmailAccount, 12)
	for i := range accounts {
		accounts[i].Email = string(rune('a'+i)) + "@example.com"
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	summary := RefreshAccounts(ctx, db, pool, accounts, 50)
	if summary.Refreshed != 0 || summary.NewCount != 0 || len(summary.Errors) != len(accounts) {
		t.Fatalf("cancelled queue reported success: %+v", summary)
	}
}
