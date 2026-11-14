package imap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/internal/models"
)

func testPool() *ConnectionPool {
	p := newConnectionPool()
	p.dial = func(context.Context, *models.EmailAccount) (*Client, error) { return &Client{}, nil }
	return p
}

func TestConnectionPool_AccountAndGlobalBoundsCancelWaiters(t *testing.T) {
	p := testPool()
	p.maxTotal = 4
	a := &models.EmailAccount{ID: 1}
	var releases []func(bool)
	for range 3 {
		_, release, err := p.AcquireConnectionContext(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := p.AcquireConnectionContext(ctx, a); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("account limit did not cancel: %v", err)
	}
	_, release, err := p.AcquireConnectionContext(context.Background(), &models.EmailAccount{ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	releases = append(releases, release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	if _, _, err := p.AcquireConnectionContext(ctx2, &models.EmailAccount{ID: 3}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("global limit did not cancel: %v", err)
	}
	if count := p.GetStats()["total_connections"].(int); count != 4 {
		t.Fatalf("expected four reserved leases, got %d", count)
	}
	for _, release := range releases {
		release(true)
		release(false)
	}
	// Idle entries count toward the bound, but can be evicted for another account.
	_, release, err = p.AcquireConnectionContext(context.Background(), &models.EmailAccount{ID: 3})
	if err != nil {
		t.Fatal(err)
	}
	release(false)
	if count := p.GetStats()["total_connections"].(int); count != 3 {
		t.Fatalf("idle eviction or idempotent release failed: %d", count)
	}
}

func TestConnectionPool_ConcurrentDialReservations(t *testing.T) {
	p := testPool()
	p.maxTotal = 5
	var active atomic.Int32
	var maximum atomic.Int32
	p.dial = func(ctx context.Context, _ *models.EmailAccount) (*Client, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := p.AcquireConnectionContext(ctx, &models.EmailAccount{ID: uint(i%3 + 1)}); err == nil {
				t.Error("cancelled dial returned success")
			}
		}()
	}
	wg.Wait()
	if maximum.Load() > 5 || active.Load() != 0 {
		t.Fatalf("dial bound/leak: max=%d active=%d", maximum.Load(), active.Load())
	}
	if count := p.GetStats()["total_connections"].(int); count != 0 {
		t.Fatalf("cancelled reservations leaked: %d", count)
	}
}

func TestConnectionPool_RemoveInvalidatesLeasedAndDialingClients(t *testing.T) {
	p := testPool()
	account := &models.EmailAccount{ID: 1}
	old, release, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	p.RemoveConnection(account.ID)
	release(true)
	if count := p.GetStats()["total_connections"].(int); count != 0 {
		t.Fatalf("invalid lease returned to pool: %d", count)
	}
	fresh, releaseFresh, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == old {
		t.Fatal("reused invalidated connection")
	}
	releaseFresh(false)
	started := make(chan struct{})
	p.dial = func(ctx context.Context, _ *models.EmailAccount) (*Client, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() { _, _, err := p.AcquireConnectionContext(context.Background(), account); done <- err }()
	<-started
	p.RemoveConnection(account.ID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("invalidated dial returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("removal failed to cancel in-flight dial")
	}
}

func TestConnectionPool_ExpiresIdleWithoutBackgroundWorkers(t *testing.T) {
	p := testPool()
	account := &models.EmailAccount{ID: 1}
	old, release, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	release(true)
	p.mu.Lock()
	for entry := range p.entries {
		entry.lastUsed = time.Now().Add(-time.Hour)
	}
	p.mu.Unlock()
	fresh, releaseFresh, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFresh(false)
	if fresh == old {
		t.Fatal("expired connection reused")
	}
}
