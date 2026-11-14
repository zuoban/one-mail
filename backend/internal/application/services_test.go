package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func applicationTestDB(t *testing.T) (*gorm.DB, *imap.ConnectionPool) {
	t.Helper()
	if err := credentials.Init(base64.StdEncoding.EncodeToString(make([]byte, 32)), ""); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "application.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.EmailAccount{}, &models.Email{}, &models.TelegramConfig{}, &models.NotificationTask{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool := imap.NewConnectionPool()
	t.Cleanup(func() {
		_ = pool.Close()
		_ = sqlDB.Close()
	})
	return db, pool
}

func ptr[T any](value T) *T {
	return &value
}

func TestCreateCustomAndPresetAccounts(t *testing.T) {
	db, pool := applicationTestDB(t)
	mailService := NewMailService(db, pool, false)
	refreshErr := errors.New("mailbox refresh unavailable")
	mailService.refreshAccount = func(_ context.Context, _ *gorm.DB, _ *imap.ConnectionPool, _ *models.EmailAccount, limit int) (int, error) {
		if limit != 50 {
			t.Fatalf("initial account refresh limit=%d, want 50", limit)
		}
		return 0, refreshErr
	}
	service := NewAccountService(db, pool, mailService)
	var tested models.EmailAccount
	service.testConnection = func(_ context.Context, account *models.EmailAccount) error {
		tested = *account
		return nil
	}
	for _, provider := range []string{"custom", "gmail"} {
		account, warning, err := service.Create(context.Background(), CreateAccount{
			Email:    "owner@example.com",
			Provider: provider,
			Username: "owner",
			Password: "private-password",
			IMAPHost: "mail.example.com",
			IMAPPort: 1993,
			SMTPHost: "legacy-smtp",
			SMTPPort: 587,
		})
		if err != nil || account == nil || !errors.Is(warning, refreshErr) {
			t.Fatalf("create result: account=%+v warning=%v err=%v", account, warning, err)
		}
		if provider == "custom" && (account.IMAPHost != "mail.example.com" || account.IMAPPort != 1993 || account.SMTPHost != "legacy-smtp") {
			t.Fatalf("custom configuration lost: %+v", account)
		}
		if provider == "gmail" && (account.IMAPHost != imap.ProviderConfigs[provider].IMAPHost || account.IMAPPort != 993) {
			t.Fatalf("preset accepted client override: %+v", account)
		}
		if tested.Password != "private-password" || tested.IMAPHost != account.IMAPHost {
			t.Fatal("connection tested wrong settings")
		}
		var saved models.EmailAccount
		if err := db.First(&saved, account.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !credentials.IsEncrypted(saved.Password) {
			t.Fatal("created password stored in plaintext")
		}
	}
}

func TestCustomAccountConstraints(t *testing.T) {
	db, pool := applicationTestDB(t)
	service := NewAccountService(db, pool, NewMailService(db, pool, false))
	service.testConnection = func(context.Context, *models.EmailAccount) error {
		t.Fatal("invalid input reached IMAP")
		return nil
	}
	for _, value := range []struct {
		host string
		port int
	}{
		{"", 993},
		{"https://mail.example.com", 993},
		{"mail.example.com:993", 993},
		{"mail.example.com/path", 993},
		{"bad host", 993},
		{"mail.example.com", 0},
		{"mail.example.com", 65536},
	} {
		_, _, err := service.Create(context.Background(), CreateAccount{
			Email:    "owner@example.com",
			Provider: "custom",
			Username: "owner",
			Password: "password",
			IMAPHost: value.host,
			IMAPPort: value.port,
		})
		kind, _ := ErrorDetails(err)
		if err == nil || kind != Invalid {
			t.Fatalf("invalid endpoint accepted: %+v %v", value, err)
		}
	}
	account := models.EmailAccount{
		Provider: "custom",
		Email:    "owner@example.com",
		Username: "owner",
		IMAPHost: "mail.example.com",
		IMAPPort: 993,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	for _, req := range []UpdateAccount{
		{IMAPHost: ptr("")},
		{IMAPPort: ptr(0)},
		{IMAPPort: ptr(65536)},
		{Username: ptr(" ")},
		{Email: ptr("")},
		{Provider: ptr("")},
	} {
		if _, err := service.Update(context.Background(), account.ID, req); err == nil {
			t.Fatalf("invalid edit accepted: %+v", req)
		}
	}
}

func TestCustomEndpointEditsClearCacheAndAdvanceVersionsAtomically(t *testing.T) {
	for _, change := range []string{"host", "port", "rollback"} {
		t.Run(change, func(t *testing.T) {
			db, pool := applicationTestDB(t)
			service := NewAccountService(db, pool, NewMailService(db, pool, false))
			account := models.EmailAccount{
				Provider:        "custom",
				Username:        "owner",
				IMAPHost:        "old.example.com",
				IMAPPort:        993,
				IdentityVersion: 2,
				ConfigVersion:   3,
				UIDValidity:     9,
				LastSyncTime:    time.Now(),
			}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{
				AccountID:       account.ID,
				MessageID:       "old",
				UID:             1,
				UIDValidity:     9,
				IdentityVersion: 2,
				BodyFetched:     true,
				BodyText:        "old private body",
			}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			if change == "rollback" {
				if err := db.Exec("CREATE TRIGGER reject_identity BEFORE UPDATE OF identity_version ON email_accounts BEGIN SELECT RAISE(FAIL, 'reject'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			req := UpdateAccount{IMAPHost: ptr("new.example.com")}
			if change == "port" {
				req = UpdateAccount{IMAPPort: ptr(1993)}
			}
			_, err := service.Update(context.Background(), account.ID, req)
			var current models.EmailAccount
			if e := db.First(&current, account.ID).Error; e != nil {
				t.Fatal(e)
			}
			var count int64
			if e := db.Model(&models.Email{}).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			if change == "rollback" {
				if err == nil || current.ConfigVersion != 3 || current.IdentityVersion != 2 || count != 1 {
					t.Fatalf("failed edit changed state: %+v count=%d err=%v", current, count, err)
				}
				return
			}
			if err != nil || current.ConfigVersion != 4 || current.IdentityVersion != 3 || current.UIDValidity != 0 || !current.LastSyncTime.IsZero() || count != 0 {
				t.Fatalf("endpoint edit retained cache/version: %+v count=%d err=%v", current, count, err)
			}
		})
	}
}

func TestBodyCommitRejectsChangedAccountAndEvictedMessage(t *testing.T) {
	for _, change := range []string{"identity", "password", "deleted", "evicted", "uidvalidity"} {
		t.Run(change, func(t *testing.T) {
			db, pool := applicationTestDB(t)
			service := NewMailService(db, pool, false)
			account := models.EmailAccount{UIDValidity: 9}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{AccountID: account.ID, UID: 1, UIDValidity: 9, MessageID: "same"}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "identity":
				err = db.Model(&account).Update("identity_version", 1).Error
			case "password":
				err = db.Model(&account).Update("config_version", 1).Error
			case "uidvalidity":
				err = db.Model(&account).Update("uid_validity", 10).Error
			case "deleted":
				err = db.Delete(&models.EmailAccount{}, account.ID).Error
			case "evicted":
				err = db.Delete(&email).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			// Restore the snapshot, because GORM updates mutate their model.
			account.ConfigVersion, account.IdentityVersion, account.UIDValidity = 0, 0, 9
			err = service.cacheEmailBody(context.Background(), &account, &email, "obsolete fetched body", "")
			if err == nil {
				t.Fatal("obsolete body committed")
			}
			if change != "deleted" && change != "evicted" && !errors.Is(err, mail.ErrAccountChanged) {
				t.Fatalf("unexpected error: %v", err)
			}
			var fetched int64
			if err := db.Model(&models.Email{}).Where("body_fetched = ?", true).Count(&fetched).Error; err != nil || fetched != 0 {
				t.Fatalf("obsolete body persisted: %d %v", fetched, err)
			}
		})
	}
}

type recordingTelegram struct {
	sent  *models.Email
	sends int
	err   error
}

func (c *recordingTelegram) TestConnection(context.Context, string, string) error {
	return c.err
}

func (c *recordingTelegram) SendNewEmailWithConfig(_ context.Context, email *models.Email, _, _, _ string) error {
	copy := *email
	c.sent = &copy
	c.sends++
	return c.err
}

func TestTelegramForwardLoadsBodyAndReusesCache(t *testing.T) {
	db, pool := applicationTestDB(t)
	account := models.EmailAccount{Email: "owner@example.com", UIDValidity: 9}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, MessageID: "forward", UID: 1, UIDValidity: 9}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: "token", ChatID: "chat"}).Error; err != nil {
		t.Fatal(err)
	}
	mailService := NewMailService(db, pool, false)
	fetches := 0
	mailService.fetchBody = func(context.Context, *models.EmailAccount, *models.Email) (string, string, error) {
		fetches++
		return "full fetched body", "", nil
	}
	client := &recordingTelegram{}
	service := NewTelegramService(db, mailService, client)
	for range 2 {
		if err := service.Forward(context.Background(), email.ID); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 || client.sends != 2 || client.sent.BodyText != "full fetched body" {
		t.Fatalf("forward did not fetch/cache body: fetches=%d sent=%+v", fetches, client.sent)
	}
	if err := db.Model(&account).Update("identity_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.Forward(context.Background(), email.ID); err == nil || client.sends != 2 {
		t.Fatal("forward sent stale cached body")
	}
}

func TestTelegramBodyFailureDoesNotSend(t *testing.T) {
	for _, failureMode := range []string{"fetch", "identity_during_fetch"} {
		t.Run(failureMode, func(t *testing.T) {
			db, pool := applicationTestDB(t)
			account := models.EmailAccount{UIDValidity: 9}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			email := models.Email{AccountID: account.ID, MessageID: "forward", UID: 1, UIDValidity: 9}
			if err := db.Create(&email).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.TelegramConfig{ID: 1, Enabled: true, BotToken: "token", ChatID: "chat"}).Error; err != nil {
				t.Fatal(err)
			}
			mailService := NewMailService(db, pool, false)
			mailService.fetchBody = func(context.Context, *models.EmailAccount, *models.Email) (string, string, error) {
				if failureMode == "fetch" {
					return "", "", fmt.Errorf("IMAP unavailable")
				}
				return "obsolete body", "", db.Model(&account).Update("identity_version", 1).Error
			}
			client := &recordingTelegram{}
			if err := NewTelegramService(db, mailService, client).Forward(context.Background(), email.ID); err == nil || client.sends != 0 {
				t.Fatalf("failed or stale body forwarded: sent=%d err=%v", client.sends, err)
			}
		})
	}
}

func TestProviderSwitchRequiresCustomEndpointAndCanonicalizesPresets(t *testing.T) {
	db, pool := applicationTestDB(t)
	service := NewAccountService(db, pool, NewMailService(db, pool, false))
	account := models.EmailAccount{Provider: "gmail", Username: "owner", IMAPHost: "imap.gmail.com", IMAPPort: 993}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	for _, req := range []UpdateAccount{
		{Provider: ptr("custom")},
		{Provider: ptr("custom"), IMAPHost: ptr("mail.example.com")},
		{Provider: ptr("custom"), IMAPPort: ptr(1993)},
	} {
		if _, err := service.Update(context.Background(), account.ID, req); err == nil {
			t.Fatal("custom transition inherited a preset endpoint")
		}
	}
	custom, err := service.Update(context.Background(), account.ID, UpdateAccount{
		Provider: ptr("custom"),
		IMAPHost: ptr("mail.example.com"),
		IMAPPort: ptr(1993),
	})
	if err != nil || custom.IMAPHost != "mail.example.com" || custom.IMAPPort != 1993 || custom.IdentityVersion != 1 || custom.ConfigVersion != 1 {
		t.Fatalf("custom switch failed: %+v %v", custom, err)
	}
	preset, err := service.Update(context.Background(), account.ID, UpdateAccount{
		Provider: ptr("outlook"),
		IMAPHost: ptr("untrusted.example.com"),
		IMAPPort: ptr(1993),
	})
	if err != nil || preset.IMAPHost != imap.ProviderConfigs["outlook"].IMAPHost || preset.IMAPPort != 993 || preset.IdentityVersion != 2 || preset.ConfigVersion != 2 {
		t.Fatalf("preset switch accepted override: %+v %v", preset, err)
	}
}

func TestAccountChangesRemoveOutboxSnapshotsAtomically(t *testing.T) {
	for _, operation := range []string{"delete", "identity", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			db, pool := applicationTestDB(t)
			service := NewAccountService(db, pool, NewMailService(db, pool, false))
			account := models.EmailAccount{Provider: "custom", Username: "owner", IMAPHost: "old.example.com", IMAPPort: 993}
			if err := db.Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			task := models.NotificationTask{
				AccountID:     account.ID,
				MessageKey:    "pending",
				AccountEmail:  "owner@example.com",
				EmailJSON:     "{}",
				NextAttemptAt: time.Now(),
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			if operation == "rollback" {
				if err := db.Exec("CREATE TRIGGER reject_identity BEFORE UPDATE OF identity_version ON email_accounts BEGIN SELECT RAISE(FAIL, 'reject'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if operation == "delete" {
				err = service.Delete(context.Background(), account.ID)
			} else {
				_, err = service.Update(context.Background(), account.ID, UpdateAccount{IMAPHost: ptr("new.example.com")})
			}
			var count int64
			if e := db.Model(&models.NotificationTask{}).Count(&count).Error; e != nil {
				t.Fatal(e)
			}
			if operation == "rollback" {
				if err == nil || count != 1 {
					t.Fatalf("failed edit discarded snapshot: count=%d err=%v", count, err)
				}
				return
			}
			if err != nil || count != 0 {
				t.Fatalf("account change retained old snapshot: count=%d err=%v", count, err)
			}
		})
	}
}

func TestPresetEditRepairsStoredEndpointAndInvalidatesIdentity(t *testing.T) {
	db, pool := applicationTestDB(t)
	service := NewAccountService(db, pool, NewMailService(db, pool, false))
	account := models.EmailAccount{
		Provider:    "gmail",
		Username:    "owner",
		IMAPHost:    "legacy-override.example.com",
		IMAPPort:    1993,
		UIDValidity: 9,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{
		AccountID:   account.ID,
		MessageID:   "legacy",
		UID:         1,
		UIDValidity: 9,
		BodyFetched: true,
		BodyText:    "legacy cached body",
	}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(context.Background(), account.ID, UpdateAccount{Email: ptr("owner@example.com")})
	if err != nil || updated.IMAPHost != imap.ProviderConfigs["gmail"].IMAPHost || updated.IMAPPort != 993 || updated.IdentityVersion != 1 || updated.ConfigVersion != 1 {
		t.Fatalf("preset endpoint was not canonicalized: %+v %v", updated, err)
	}
	var count int64
	if err := db.Model(&models.Email{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("canonicalized preset retained old body: count=%d err=%v", count, err)
	}
}

func TestRefreshAccountPassesConfiguredWindow(t *testing.T) {
	db, pool := applicationTestDB(t)
	account := models.EmailAccount{Email: "window@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	service := NewMailService(db, pool, false)
	for _, tc := range []struct{ requested, want int }{{0, 50}, {25, 25}, {100, 100}, {200, 200}} {
		service.refreshAccount = func(_ context.Context, _ *gorm.DB, _ *imap.ConnectionPool, got *models.EmailAccount, limit int) (int, error) {
			if got.ID != account.ID || limit != tc.want {
				t.Fatalf("refresh account=%d limit=%d, want account=%d limit=%d", got.ID, limit, account.ID, tc.want)
			}
			return limit, nil
		}
		if count, err := service.RefreshAccount(context.Background(), account.ID, tc.requested); err != nil || count != tc.want {
			t.Fatalf("requested=%d: count=%d err=%v", tc.requested, count, err)
		}
	}
}

func TestBodyIdentityRecoveryPreservesExpandedWindow(t *testing.T) {
	db, pool := applicationTestDB(t)
	account := models.EmailAccount{Email: "recovery@example.com"}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	var cached []models.Email
	var summaries []*imap.EmailSummary
	for uid := 1; uid <= 101; uid++ {
		summaries = append(summaries, &imap.EmailSummary{UID: uint(uid), Date: base.Add(time.Duration(uid) * time.Minute)})
		if uid <= 100 {
			cached = append(cached, models.Email{AccountID: account.ID, Folder: mail.DefaultFolder, UID: uint(uid), UIDValidity: 7, BodyFetched: true, BodyText: fmt.Sprintf("cached body %d", uid)})
		}
	}
	if err := db.Create(&cached).Error; err != nil {
		t.Fatal(err)
	}
	legacy := models.Email{AccountID: account.ID, Folder: mail.DefaultFolder, UID: 101}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	service := NewMailService(db, pool, false)
	service.refreshAccount = func(_ context.Context, db *gorm.DB, _ *imap.ConnectionPool, account *models.EmailAccount, limit int) (int, error) {
		if limit != 200 {
			t.Fatalf("identity recovery limit=%d, want maximum supported 200", limit)
		}
		return mail.ReconcileLatestEmails(db, account, mail.DefaultFolder, 7, summaries, limit)
	}
	service.fetchBody = func(context.Context, *models.EmailAccount, *models.Email) (string, string, error) {
		t.Fatal("legacy ID must be reloaded after identity reconciliation")
		return "", "", nil
	}
	_, err := service.Get(context.Background(), legacy.ID)
	if kind, _ := ErrorDetails(err); err == nil || kind != NotFound {
		t.Fatalf("legacy identity was reused after reconciliation: %v", err)
	}
	for _, original := range cached {
		var stored models.Email
		if err := db.First(&stored, original.ID).Error; err != nil || stored.UID != original.UID || !stored.BodyFetched || stored.BodyText != original.BodyText {
			t.Fatalf("recovery lost cached ID/body: %+v err=%v", stored, err)
		}
	}
	var count int64
	if err := db.Model(&models.Email{}).Count(&count).Error; err != nil || count != 101 {
		t.Fatalf("identity recovery shrank expanded window: count=%d err=%v", count, err)
	}
}
