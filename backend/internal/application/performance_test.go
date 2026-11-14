package application

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func benchmarkMailbox(b *testing.B) (*gorm.DB, *imap.ConnectionPool, []models.EmailAccount, []models.Email) {
	b.Helper()
	previousOutput := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(previousOutput) })
	db, err := gorm.Open(sqlite.Open(filepath.Join(b.TempDir(), "benchmark.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		b.Fatal(err)
	}
	if err := database.SetupSQLite(db, true); err != nil {
		b.Fatal(err)
	}
	if err := database.InitializeSchema(db); err != nil {
		b.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	pool := imap.NewConnectionPool()
	b.Cleanup(func() { _ = pool.Close(); _ = sqlDB.Close() })
	accounts := make([]models.EmailAccount, 10)
	for index := range accounts {
		accounts[index] = models.EmailAccount{
			Email: fmt.Sprintf("benchmark-%d@example.com", index), UIDValidity: 7,
			LastSyncTime: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		}
	}
	if err := db.Create(&accounts).Error; err != nil {
		b.Fatal(err)
	}
	rows := make([]models.Email, 0, 500)
	body := strings.Repeat("cached-content-", 512)
	for accountIndex, account := range accounts {
		for messageIndex := range 50 {
			index := accountIndex*50 + messageIndex
			rows = append(rows, models.Email{
				AccountID: account.ID, UID: uint(messageIndex + 1), UIDValidity: 7,
				Folder: "INBOX", MessageID: fmt.Sprintf("benchmark-%d", index),
				Subject: fmt.Sprintf("Invoice %04d literal_100%%", index),
				From:    "sender@example.com", FromName: "Sender", To: account.Email,
				Date:        account.LastSyncTime.Add(time.Duration(index) * time.Minute),
				BodyFetched: true, BodyText: body,
			})
		}
	}
	if err := db.CreateInBatches(&rows, 100).Error; err != nil {
		b.Fatal(err)
	}
	return db, pool, accounts, rows
}

func BenchmarkMailList(b *testing.B) {
	db, pool, accounts, _ := benchmarkMailbox(b)
	for _, scenario := range []struct {
		name  string
		query ListEmails
		fts   bool
	}{
		{"all", ListEmails{Limit: 50}, false},
		{"account", ListEmails{AccountID: accounts[0].ID, Limit: 50}, false},
		{"search_like", ListEmails{Search: "Invoice", Limit: 50}, false},
		{"search_fts5", ListEmails{Search: "Invoice", Limit: 50}, true},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			if scenario.fts && !database.IsFTS5Enabled() {
				b.Skip("FTS5 requires -tags fts5")
			}
			service := NewMailService(db, pool, scenario.fts)
			b.ReportAllocs()
			for b.Loop() {
				result, err := service.List(context.Background(), scenario.query)
				if err != nil || len(result) != 50 {
					b.Fatalf("list benchmark failed: count=%d error=%v", len(result), err)
				}
			}
		})
	}
}

func BenchmarkCachedBody(b *testing.B) {
	db, pool, _, rows := benchmarkMailbox(b)
	service := NewMailService(db, pool, false)
	index := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		body, err := service.Get(context.Background(), rows[index%len(rows)].ID)
		if err != nil || !body.BodyFetched {
			b.Fatalf("cached body failed: %v", err)
		}
		index++
	}
}

func BenchmarkFirstBodyMock(b *testing.B) {
	db, pool, _, rows := benchmarkMailbox(b)
	service := NewMailService(db, pool, false)
	var fetches atomic.Uint64
	service.fetchBody = func(ctx context.Context, _ *models.EmailAccount, _ *models.Email) (string, string, error) {
		fetches.Add(1)
		return rows[0].BodyText, "", ctx.Err()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		err := db.Model(&models.Email{}).Where("id = ?", rows[0].ID).
			Updates(map[string]interface{}{"body_fetched": false, "body_text": ""}).Error
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		body, err := service.Get(context.Background(), rows[0].ID)
		if err != nil || body.BodyText != rows[0].BodyText {
			b.Fatalf("first mock body failed: %v", err)
		}
	}
	b.ReportMetric(float64(fetches.Load())/float64(b.N), "fetches/op")
}

func BenchmarkRefreshReconcileParallel(b *testing.B) {
	db, _, accounts, rows := benchmarkMailbox(b)
	batches := make([][]*imap.EmailSummary, len(accounts))
	for _, row := range rows {
		index := int(row.AccountID - accounts[0].ID)
		batches[index] = append(batches[index], &imap.EmailSummary{
			UID: row.UID, MessageID: row.MessageID, Subject: row.Subject, From: row.From,
			FromName: row.FromName, To: row.To, Date: row.Date,
		})
	}
	locks := make([]sync.Mutex, len(accounts))
	slots := make(chan struct{}, 4)
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			index := int(next.Add(1)-1) % len(accounts)
			locks[index].Lock()
			slots <- struct{}{}
			account := accounts[index]
			count, err := mail.ReconcileLatestEmails(db, &account, "INBOX", 7, batches[index], 50)
			<-slots
			locks[index].Unlock()
			if err != nil || count != 0 {
				b.Errorf("unchanged refresh transaction failed: count=%d error=%v", count, err)
			}
		}
	})
}
