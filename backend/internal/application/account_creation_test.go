package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"

	"gorm.io/gorm"
)

func creationService(t *testing.T) (*gorm.DB, *AccountService) {
	t.Helper()
	db, pool := applicationTestDB(t)
	mailService := NewMailService(db, pool, false)
	mailService.refreshAccount = func(context.Context, *gorm.DB, *imap.ConnectionPool, *models.EmailAccount, int) (int, error) {
		return 0, nil
	}
	service := NewAccountService(db, pool, mailService)
	service.testConnection = func(context.Context, *models.EmailAccount) error { return nil }
	return db, service
}

func creationInput() CreateAccount {
	return CreateAccount{
		Email: "owner@example.com", Provider: "custom", Username: "owner",
		Password: "test-password", IMAPHost: "imap.example.com", IMAPPort: 993,
	}
}

func TestCreationBudgetAndSavedAccountSurviveInitialSyncCancellation(t *testing.T) {
	db, service := creationService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var connections atomic.Int32
	service.testConnection = func(ctx context.Context, account *models.EmailAccount) error {
		connections.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 25*time.Second {
			t.Error("connection test has no overall creation budget")
		}
		return nil
	}
	service.mail.refreshAccount = func(ctx context.Context, _ *gorm.DB, _ *imap.ConnectionPool, account *models.EmailAccount, _ int) (int, error) {
		var saved models.EmailAccount
		if err := db.First(&saved, account.ID).Error; err != nil {
			t.Fatal("initial sync started before the account was saved")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Error("initial sync has no bounded budget")
		}
		cancel()
		return 0, ctx.Err()
	}
	account, warning, err := service.Create(ctx, creationInput())
	if err != nil || account == nil || !errors.Is(warning, context.Canceled) {
		t.Fatalf("saved creation lost its independent outcome: account=%v warning=%v error=%v", account, warning, err)
	}
	_, _, err = service.Create(context.Background(), creationInput())
	kind, _ := ErrorDetails(err)
	if err == nil || kind != Conflict || connections.Load() != 1 {
		t.Fatalf("retry did not detect saved mailbox before connecting: connections=%d error=%v", connections.Load(), err)
	}
	var count int64
	if err := db.Model(&models.EmailAccount{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retry duplicated the saved account: count=%d error=%v", count, err)
	}
}

func TestConcurrentCreationRejectsDuplicateMailbox(t *testing.T) {
	db, service := creationService(t)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	service.testConnection = func(ctx context.Context, _ *models.EmailAccount) error {
		ready <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, _, err := service.Create(context.Background(), creationInput())
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("concurrent validation did not start")
		}
	}
	close(release)
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if kind, _ := ErrorDetails(err); kind == Conflict {
			conflicts++
		} else {
			t.Fatalf("unexpected creation error: %v", err)
		}
	}
	var count int64
	if err := db.Model(&models.EmailAccount{}).Count(&count).Error; err != nil || count != 1 || success != 1 || conflicts != 1 {
		t.Fatalf("duplicate admission failed: count=%d success=%d conflicts=%d error=%v", count, success, conflicts, err)
	}
}

func TestMailboxUniquenessCanonicalizesHostAndAllowsRecreationAfterDelete(t *testing.T) {
	_, service := creationService(t)
	input := creationInput()
	input.IMAPHost = "IMAP.Example.COM"
	account, _, err := service.Create(context.Background(), input)
	if err != nil || account.IMAPHost != "imap.example.com" {
		t.Fatalf("host was not canonicalized: account=%v error=%v", account, err)
	}
	_, _, err = service.Create(context.Background(), creationInput())
	if kind, _ := ErrorDetails(err); err == nil || kind != Conflict {
		t.Fatalf("host case bypassed duplicate detection: %v", err)
	}
	other := creationInput()
	other.Username = "other"
	second, _, err := service.Create(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), second.ID, UpdateAccount{Username: ptr(input.Username)}); err == nil {
		t.Fatal("account update introduced a duplicate mailbox")
	}
	if err := service.Delete(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Create(context.Background(), creationInput()); err != nil {
		t.Fatalf("deleted mailbox could not be recreated: %v", err)
	}
}

func TestCancelledCreationNeverTestsOrSavesMailbox(t *testing.T) {
	db, service := creationService(t)
	service.testConnection = func(context.Context, *models.EmailAccount) error {
		t.Error("cancelled creation started a connection test")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if account, _, err := service.Create(ctx, creationInput()); err == nil || account != nil {
		t.Fatalf("cancelled creation succeeded: %v %v", account, err)
	}
	var count int64
	if err := db.Model(&models.EmailAccount{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("cancelled creation persisted an account: %d %v", count, err)
	}
}

func TestAccountWriteWaitCanBeCancelled(t *testing.T) {
	_, service := creationService(t)
	service.writes <- struct{}{}
	defer func() { <-service.writes }()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Update(ctx, 1, UpdateAccount{Email: ptr("changed@example.com")})
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("write wait lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled write remained blocked")
	}
}

func TestExistingDuplicateMailboxesCanStillRotateCredentials(t *testing.T) {
	db, service := creationService(t)
	for range 2 {
		if err := db.Create(&models.EmailAccount{
			Email: "owner@example.com", Provider: "custom", Username: "owner",
			IMAPHost: "imap.example.com", IMAPPort: 993,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	account, err := service.Update(context.Background(), 1, UpdateAccount{Password: ptr("rotated-password")})
	if err != nil || account.ConfigVersion != 1 || account.IdentityVersion != 0 {
		t.Fatalf("duplicate detection blocked credential rotation: %v %v", account, err)
	}
}
