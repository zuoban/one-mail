package sync

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
