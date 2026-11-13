package sync

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

func TestGetBatchSizeAndBatchedCacheRebuild(t *testing.T) {
	const testAccID = uint(98765)
	accountLatency.Delete(testAccID)

	if got := getBatchSize(testAccID, true); got != 500 {
		t.Fatalf("expected first sync batch=500, got %d", got)
	}
	if got := getBatchSize(testAccID, false); got != 100 {
		t.Fatalf("expected default incremental batch=100, got %d", got)
	}

	accountLatency.Store(testAccID, 300*time.Millisecond)
	if got := getBatchSize(testAccID, false); got != 200 {
		t.Fatalf("expected fast network batch=200, got %d", got)
	}

	accountLatency.Store(testAccID, 1500*time.Millisecond)
	if got := getBatchSize(testAccID, false); got != 100 {
		t.Fatalf("expected medium network batch=100, got %d", got)
	}

	accountLatency.Store(testAccID, 4*time.Second)
	if got := getBatchSize(testAccID, false); got != 50 {
		t.Fatalf("expected slow network batch=50, got %d", got)
	}
	accountLatency.Delete(testAccID)

	dbPath := filepath.Join(t.TempDir(), "worker_test.db")
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
	u := models.User{Username: "syncuser", Password: "p", Email: "sync@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	acc := models.EmailAccount{ID: testAccID, UserID: u.ID, Email: "sync@example.com", Provider: "custom", IMAPHost: "imap.example.com", IMAPPort: 993, SMTPHost: "smtp.example.com", SMTPPort: 587, Username: "sync", Password: "p"}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatal(err)
	}

	var batch []models.Email
	for i := range 25 {
		batch = append(batch, models.Email{
			AccountID: testAccID,
			MessageID: fmt.Sprintf("msg-%d", i),
			From:      "a@b.com",
			To:        "c@d.com",
			Date:      time.Now(),
			Folder:    "INBOX",
			UID:       uint(i + 1),
		})
	}
	if err := db.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}

	cache := GetSyncCache()
	defer cache.Clear(testAccID)

	total, err := rebuildAccountCacheBatched(db, cache, testAccID)
	if err != nil {
		t.Fatalf("rebuildAccountCacheBatched failed: %v", err)
	}
	if total != 25 {
		t.Fatalf("expected total=25, got %d", total)
	}
	for i := range 25 {
		id := fmt.Sprintf("msg-%d", i)
		if !cache.MightContain(testAccID, id) {
			t.Fatalf("expected bloom filter to contain %s", id)
		}
	}
}

func TestParseSyncFoldersAndBackoff(t *testing.T) {
	if got := parseSyncFolders(""); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("expected default [INBOX], got %v", got)
	}
	if got := parseSyncFolders(" , , "); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("expected default [INBOX] for whitespace, got %v", got)
	}
	if got := parseSyncFolders("Sent, inbox, Archive, INBOX, Sent "); !reflect.DeepEqual(got, []string{"inbox", "Sent", "Archive"}) {
		t.Fatalf("expected INBOX-first deduplicated folders, got %v", got)
	}

	authErr := errors.New("failed to login: Authentication failed")
	if !isAuthError(authErr) {
		t.Fatal("expected auth error classification")
	}
	netErr := errors.New("read tcp: i/o timeout")
	if isAuthError(netErr) || !isNetworkError(netErr) {
		t.Fatal("expected network error classification")
	}

	if d := computeRetryBackoff(1, authErr); d != 15*time.Minute {
		t.Fatalf("expected 15m auth backoff, got %v", d)
	}
	if d := computeRetryBackoff(2, authErr); d != 30*time.Minute {
		t.Fatalf("expected 30m auth backoff, got %v", d)
	}
	if d := computeRetryBackoff(1, netErr); d != 1*time.Minute {
		t.Fatalf("expected 1m transient backoff, got %v", d)
	}
	if d := computeRetryBackoff(3, netErr); d != 4*time.Minute {
		t.Fatalf("expected 4m transient backoff, got %v", d)
	}
	if d := computeRetryBackoff(10, netErr); d != 15*time.Minute {
		t.Fatalf("expected capped 15m transient backoff, got %v", d)
	}
}

func TestShouldSyncFlags(t *testing.T) {
	key := folderSyncKey{accountID: 777, folder: "INBOX"}
	lastFlagSyncAt.Delete(key)
	defer lastFlagSyncAt.Delete(key)

	if shouldSyncFlags(key, &models.SyncState{LastUID: 0}, 10) {
		t.Fatal("should skip flag sync before initial LastUID is set")
	}
	if shouldSyncFlags(key, &models.SyncState{LastUID: 100, HighestModSeq: 42}, 42) {
		t.Fatal("should skip flag sync when CONDSTORE HighestModSeq is unchanged")
	}
	if !shouldSyncFlags(key, &models.SyncState{LastUID: 100, HighestModSeq: 42}, 43) {
		t.Fatal("should run flag sync when CONDSTORE HighestModSeq changed")
	}

	if !shouldSyncFlags(key, &models.SyncState{LastUID: 100}, 0) {
		t.Fatal("should run flag sync on first non-CONDSTORE check")
	}
	lastFlagSyncAt.Store(key, time.Now())
	if shouldSyncFlags(key, &models.SyncState{LastUID: 100}, 0) {
		t.Fatal("should throttle non-CONDSTORE flag sync within interval")
	}
}
