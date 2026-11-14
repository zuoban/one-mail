package imap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/internal/models"
)

// Exercise actual IMAP command encoding, decoding and transport cancellation
// using the same isolated protocol peer as the mailbox tests.
func protocolPool(t *testing.T, respond func(*models.EmailAccount, string, string) string) *ConnectionPool {
	t.Helper()
	p := NewConnectionPool()
	t.Cleanup(func() { _ = p.Close() })
	p.dial = func(ctx context.Context, account *models.EmailAccount) (*Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client := scriptedClient(t, func(tag, command string) string {
			if respond != nil {
				if response := respond(account, tag, command); response != "" {
					return response
				}
			}
			if strings.EqualFold(command, "LOGOUT") {
				return fmt.Sprintf("* BYE closing\r\n%s OK logout\r\n", tag)
			}
			return fmt.Sprintf("%s OK completed\r\n", tag)
		})
		client.lastActivity = time.Now()
		return client, nil
	}
	return p
}

func testAccount(id uint) *models.EmailAccount {
	return &models.EmailAccount{ID: id, IMAPHost: "mail.example.test", IMAPPort: 993, Username: "reader", Password: "secret"}
}

func TestAdmission_PooledAndTemporaryShareGlobalCapacity(t *testing.T) {
	logoutStarted := make(chan struct{}, 1)
	unblockLogout := make(chan struct{})
	defer close(unblockLogout)
	p := protocolPool(t, func(account *models.EmailAccount, tag, command string) string {
		if account.ID == 16 && strings.EqualFold(command, "LOGOUT") {
			logoutStarted <- struct{}{}
			<-unblockLogout
			return fmt.Sprintf("* BYE closing\r\n%s OK logout\r\n", tag)
		}
		return ""
	})
	var releases []func(bool)
	for id := uint(1); id <= 15; id++ {
		_, release, err := p.AcquireConnectionContext(context.Background(), testAccount(id))
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.TestConnectionContext(ctx, testAccount(16)) }()
	select {
	case <-logoutStarted:
	case <-time.After(time.Second):
		t.Fatal("temporary connection did not reach IMAP logout")
	}
	stats := p.GetStats()
	if stats["total_connections"] != 16 || stats["temporary_connections"] != 1 {
		t.Fatalf("temporary connection missing from shared ledger: %v", stats)
	}
	for _, kind := range []string{"pooled", "temporary", "direct"} {
		waitCtx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
		var err error
		switch kind {
		case "temporary":
			err = p.TestConnectionContext(waitCtx, testAccount(17))
		case "direct":
			err = NewClient(testAccount(17), p).ConnectContext(waitCtx)
		default:
			_, _, err = p.AcquireConnectionContext(waitCtx, testAccount(17))
		}
		stop()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shared global admission failed, kind=%s: %v", kind, err)
		}
	}
	// A temporary test must reclaim an idle pool slot instead of waiting forever.
	releases[0](true)
	waitCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := p.TestConnectionContext(waitCtx, testAccount(17)); err != nil {
		t.Fatalf("temporary test did not evict idle pooled transport: %v", err)
	}
	if p.GetStats()["total_connections"] != 15 {
		t.Fatal("temporary completion or idle eviction leaked a slot")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("temporary logout lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("temporary logout ignored cancellation")
	}
	for _, release := range releases {
		release(false)
	}
	if p.GetStats()["total_connections"] != 0 {
		t.Fatal("cancelled temporary connection leaked admission")
	}
}

func TestAdmission_StableUnsavedIdentityAndSavedAccountLimit(t *testing.T) {
	for _, id := range []uint{0, 9} {
		t.Run(fmt.Sprintf("account_%d", id), func(t *testing.T) {
			p := protocolPool(t, nil)
			account := testAccount(id)
			var releases []func(bool)
			for i := range 3 {
				// Mix temporary and pooled leases for a saved account. Unsaved
				// clients always use temporary reservations, keyed by endpoint/user.
				temporary := id == 0 || i == 2
				_, release, err := p.acquire(context.Background(), account, temporary, p.dial)
				if err != nil {
					t.Fatal(err)
				}
				releases = append(releases, release)
			}
			copy := *account
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			copy.IMAPHost = strings.ToUpper(copy.IMAPHost)
			defer cancel()
			if err := p.TestConnectionContext(ctx, &copy); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("fourth connection bypassed stable account limit: %v", err)
			}
			if p.GetStats()["total_connections"] != 3 {
				t.Fatal("cancelled waiter leaked admission")
			}
			other := testAccount(0)
			other.Username = "another-reader"
			ctxOther, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := p.TestConnectionContext(ctxOther, other); err != nil {
				t.Fatalf("unrelated unsaved identity was throttled: %v", err)
			}
			for _, release := range releases {
				release(false)
			}
			if p.GetStats()["total_connections"] != 0 {
				t.Fatal("temporary reservations were retained")
			}
		})
	}
}

func TestAdmission_IdleHealthCheckRebuildsAndSkipsRecentActivity(t *testing.T) {
	var noops, dials atomic.Int32
	p := protocolPool(t, func(_ *models.EmailAccount, tag, command string) string {
		if strings.EqualFold(command, "NOOP") {
			noops.Add(1)
			if dials.Load() == 1 {
				return fmt.Sprintf("%s NO connection expired\r\n", tag)
			}
		}
		return ""
	})
	baseDial := p.dial
	p.dial = func(ctx context.Context, account *models.EmailAccount) (*Client, error) {
		dials.Add(1)
		return baseDial(ctx, account)
	}
	account := testAccount(1)
	old, release, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	release(true)
	recent, releaseRecent, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil || recent != old || noops.Load() != 0 {
		t.Fatalf("recent connection added a health RTT: reused=%v, err=%v, noops=%d", recent == old, err, noops.Load())
	}
	old.lastActivity = time.Now().Add(-time.Minute)
	releaseRecent(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fresh, releaseFresh, err := p.AcquireConnectionContext(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFresh(false)
	if fresh == old || noops.Load() != 1 || dials.Load() != 2 || p.GetStats()["total_connections"] != 1 {
		t.Fatalf("expired lease was not replaced within one slot: reused=%v noops=%d dials=%d stats=%v", fresh == old, noops.Load(), dials.Load(), p.GetStats())
	}
	if err := fresh.noopContext(ctx); err != nil {
		t.Fatalf("replacement transport failed its IMAP command: %v", err)
	}
}

func TestAdmission_CancelledHealthCheckDoesNotRedial(t *testing.T) {
	noopStarted := make(chan struct{}, 1)
	unblockNoop := make(chan struct{})
	defer close(unblockNoop)
	var dials atomic.Int32
	p := protocolPool(t, func(_ *models.EmailAccount, tag, command string) string {
		if strings.EqualFold(command, "NOOP") {
			noopStarted <- struct{}{}
			<-unblockNoop
			return fmt.Sprintf("%s OK noop\r\n", tag)
		}
		return ""
	})
	baseDial := p.dial
	p.dial = func(ctx context.Context, account *models.EmailAccount) (*Client, error) {
		dials.Add(1)
		return baseDial(ctx, account)
	}
	account := testAccount(1)
	client, release, err := p.AcquireConnectionContext(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	client.lastActivity = time.Now().Add(-time.Minute)
	release(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := p.AcquireConnectionContext(ctx, account); done <- err }()
	select {
	case <-noopStarted:
	case <-time.After(time.Second):
		t.Fatal("idle connection was not checked")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("health check lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("health NOOP ignored cancellation")
	}
	if dials.Load() != 1 || p.GetStats()["total_connections"] != 0 {
		t.Fatalf("cancelled health check redialed or leaked admission: dials=%d stats=%v", dials.Load(), p.GetStats())
	}
}

func TestAdmission_CloseClosesLeasedIdleAndWakesWaiters(t *testing.T) {
	p := protocolPool(t, nil)
	p.maxTotal = 2
	leased, releaseLeased, err := p.AcquireConnectionContext(context.Background(), testAccount(1))
	if err != nil {
		t.Fatal(err)
	}
	idle, releaseIdle, err := p.AcquireConnectionContext(context.Background(), testAccount(2))
	if err != nil {
		t.Fatal(err)
	}
	releaseIdle(true)
	// A separate fully leased pool ensures the capacity waiter cannot evict idle.
	waitingPool := protocolPool(t, nil)
	waitingPool.maxTotal = 1
	_, releaseWaiting, err := waitingPool.AcquireConnectionContext(context.Background(), testAccount(1))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := waitingPool.AcquireConnectionContext(context.Background(), testAccount(2))
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for waitingPool.GetStats()["wait_count"].(uint64) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("acquisition did not wait for capacity")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitingPool.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("closed waiter returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not wake waiter")
	}
	for _, client := range []*Client{leased, idle} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := client.noopContext(ctx); err == nil {
			t.Error("Close left an established transport alive")
		}
		cancel()
	}
	releaseLeased(true)
	releaseWaiting(true)
	if p.GetStats()["total_connections"] != 0 || waitingPool.GetStats()["total_connections"] != 0 {
		t.Fatal("late releases resurrected closed connections")
	}
	if _, _, err := p.AcquireConnectionContext(context.Background(), testAccount(3)); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("closed pool admitted a connection: %v", err)
	}
	if err := p.TestConnectionContext(context.Background(), testAccount(3)); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("closed pool admitted a temporary connection: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close is not idempotent: %v", err)
	}
}

func TestAdmission_CloseCancelsDialingReservations(t *testing.T) {
	p := NewConnectionPool()
	started := make(chan struct{})
	p.dial = func(ctx context.Context, account *models.EmailAccount) (*Client, error) {
		client := scriptedClient(t, func(tag, command string) string {
			close(started)
			return ""
		})
		defer client.ForceClose()
		// A greeting followed by a stalled NOOP exercises protocol cancellation
		// while the pool still considers this a dialing reservation.
		return nil, client.noopContext(ctx)
	}
	done := make(chan error, 1)
	go func() { _, _, err := p.AcquireConnectionContext(context.Background(), testAccount(1)); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dial did not start")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("cancelled dial returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel dial")
	}
	if p.GetStats()["total_connections"] != 0 {
		t.Fatal("Close leaked dialing reservation")
	}
}

func TestAdmission_CloseCancelsTemporaryProtocolTest(t *testing.T) {
	started := make(chan struct{})
	unblock := make(chan struct{})
	defer close(unblock)
	p := protocolPool(t, func(_ *models.EmailAccount, tag, command string) string {
		if strings.EqualFold(command, "LOGOUT") {
			close(started)
			<-unblock
			return fmt.Sprintf("* BYE closing\r\n%s OK logout\r\n", tag)
		}
		return ""
	})
	done := make(chan error, 1)
	go func() { done <- p.TestConnectionContext(context.Background(), testAccount(0)) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("temporary test did not reach its protocol peer")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown allowed stalled temporary test to succeed")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt temporary protocol command")
	}
	if p.GetStats()["total_connections"] != 0 || p.GetStats()["temporary_connections"] != 0 {
		t.Fatal("shutdown retained a temporary transport")
	}
}
