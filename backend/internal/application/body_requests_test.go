package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/internal/models"
)

func TestColdBodyRequestsShareWorkWithIndependentCancellation(t *testing.T) {
	db, pool := applicationTestDB(t)
	account := models.EmailAccount{UIDValidity: 9}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	email := models.Email{AccountID: account.ID, UID: 1, UIDValidity: 9, Folder: "INBOX"}
	if err := db.Create(&email).Error; err != nil {
		t.Fatal(err)
	}
	service := NewMailService(db, pool, false)
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	service.fetchBody = func(ctx context.Context, _ *models.EmailAccount, _ *models.Email) (string, string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-finish:
			return "shared body", "", nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := service.Get(ctx, email.ID); leader <- err }()
	<-started
	follower := make(chan *models.Email, 1)
	go func() {
		body, _ := service.Get(context.Background(), email.ID)
		follower <- body
	}()
	deadline := time.After(time.Second)
	for {
		if _, shared := service.bodyRequests.Stats(); shared > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("body follower did not join")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled body request returned %v", err)
	}
	close(finish)
	if body := <-follower; body == nil || body.BodyText != "shared body" || !body.BodyFetched || calls.Load() != 1 {
		t.Fatalf("follower body=%+v fetches=%d", body, calls.Load())
	}
	if body, err := service.Get(context.Background(), email.ID); err != nil || body.BodyText != "shared body" || calls.Load() != 1 {
		t.Fatalf("completed body was not cached: %+v %v", body, err)
	}
}
