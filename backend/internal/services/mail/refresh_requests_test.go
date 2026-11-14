package mail

import (
	"context"
	"errors"
	"testing"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
)

func TestConcurrentRefreshSharesAccountWorkAndIsolatesCancellation(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{UIDValidity: 8}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	unlock, err := lockAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	_, before := RefreshStats()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	first, second := account, account
	go func() { _, err := RefreshAccount(ctx, db, pool, &first, 50); firstDone <- err }()
	go func() { _, err := RefreshAccount(context.Background(), db, pool, &second, 50); secondDone <- err }()
	deadline := time.After(time.Second)
	for {
		if _, shared := RefreshStats(); shared > before {
			break
		}
		select {
		case <-deadline:
			t.Fatal("refresh requests did not merge")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter returned %v", err)
	}
	if err := db.Model(&account).Update("config_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock = nil
	if err := <-secondDone; !errors.Is(err, ErrAccountChanged) {
		t.Fatalf("live follower was cancelled or skipped identity validation: %v", err)
	}
}

func TestCachedSummariesExcludeOldIdentityAndKeepBodyOutOfProjection(t *testing.T) {
	db := setupFetcherTestDB(t)
	account := models.EmailAccount{UIDValidity: 8, IdentityVersion: 2}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	rows := []models.Email{
		{AccountID: account.ID, Folder: DefaultFolder, UID: 1, UIDValidity: 8, IdentityVersion: 2, Subject: "current", BodyText: "private body"},
		{AccountID: account.ID, Folder: DefaultFolder, UID: 2, UIDValidity: 7, IdentityVersion: 2},
		{AccountID: account.ID, Folder: DefaultFolder, UID: 3, UIDValidity: 8, IdentityVersion: 1},
		{AccountID: account.ID, Folder: "Archive", UID: 4, UIDValidity: 8, IdentityVersion: 2},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	result, err := cachedSummaries(db, &account)
	if err != nil || len(result) != 1 || result[1].Subject != "current" {
		t.Fatalf("cache reused unrelated metadata: %+v %v", result, err)
	}
}
