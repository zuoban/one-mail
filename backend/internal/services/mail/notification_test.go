package mail

import (
	"encoding/json"
	"testing"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

func TestInitializedEmptyMailboxNotifiesFirstArrival(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "empty@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: "encrypted-token", ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, nil, 50); err != nil {
		t.Fatal(err)
	}
	batch := []*imap.EmailSummary{{UID: 1, Subject: "first real arrival", Date: time.Now()}}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch, 50); err != nil {
		t.Fatal(err)
	}
	var tasks []models.NotificationTask
	if err := db.Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].AccountID != account.ID || tasks[0].AccountEmail != account.Email {
		t.Fatalf("empty mailbox first arrival lost: %+v", tasks)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch, 50); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.NotificationTask{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unchanged refresh duplicated delivery: count=%d err=%v", count, err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 8, []*imap.EmailSummary{{UID: 2}}, 50); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.NotificationTask{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("new UIDVALIDITY replayed history: count=%d err=%v", count, err)
	}
}

func TestNotificationFailureRollsBackMailCommit(t *testing.T) {
	db := setupFetcherTestDB(t)
	previous := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	account := models.EmailAccount{Email: "atomic@example.com", UIDValidity: 7, LastSyncTime: previous}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: "encrypted-token", ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_outbox BEFORE INSERT ON notification_tasks BEGIN SELECT RAISE(FAIL, 'outbox unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, []*imap.EmailSummary{{UID: 1}}, 50); err == nil {
		t.Fatal("mail committed without notification task")
	}
	var count int64
	if err := db.Model(&models.Email{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed outbox left committed mail: count=%d err=%v", count, err)
	}
	var stored models.EmailAccount
	if err := db.First(&stored, account.ID).Error; err != nil || !stored.LastSyncTime.Equal(previous) {
		t.Fatalf("failed outbox advanced baseline: %+v %v", stored, err)
	}
}

func TestInitialImportAndDisabledNotificationsDoNotQueueHistory(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "import@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	cfg := models.TelegramConfig{ID: 1, Enabled: true, BotToken: "encrypted-token", ChatID: "chat"}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, []*imap.EmailSummary{{UID: 1}}, 50); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&cfg).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, []*imap.EmailSummary{{UID: 1}, {UID: 2}}, 50); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.NotificationTask{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("historical or disabled notification queued: count=%d err=%v", count, err)
	}
}

func TestExpandedWindowBackfillDoesNotNotifyHistoricalUIDs(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{Email: "backfill@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: "token", ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	batch := func(first, last int) []*imap.EmailSummary {
		var result []*imap.EmailSummary
		for uid := first; uid <= last; uid++ {
			// Dates deliberately oppose UID order: arrival detection uses UID.
			result = append(result, &imap.EmailSummary{UID: uint(uid), Date: base.Add(-time.Duration(uid) * time.Minute)})
		}
		return result
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(101, 150), 50); err != nil || count != 50 {
		t.Fatalf("initial import: count=%d err=%v", count, err)
	}
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(51, 150), 100); err != nil || count != 50 {
		t.Fatalf("historical backfill: count=%d err=%v", count, err)
	}
	var tasks []models.NotificationTask
	if err := db.Find(&tasks).Error; err != nil || len(tasks) != 0 {
		t.Fatalf("backfill queued historical notifications: %+v err=%v", tasks, err)
	}
	// Another expansion includes both historical UIDs and one actual new arrival.
	if count, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(1, 151), 200); err != nil || count != 51 {
		t.Fatalf("arrival with expanded window: count=%d err=%v", count, err)
	}
	if _, err := ReconcileLatestEmails(db, &account, DefaultFolder, 7, batch(1, 151), 200); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&tasks).Error; err != nil || len(tasks) != 1 {
		t.Fatalf("expected exactly one arrival notification: %+v err=%v", tasks, err)
	}
	var delivered models.Email
	if err := json.Unmarshal([]byte(tasks[0].EmailJSON), &delivered); err != nil || delivered.UID != 151 {
		t.Fatalf("wrong notification payload: %+v err=%v", delivered, err)
	}
}
