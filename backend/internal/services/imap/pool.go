package imap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"one-mail/backend/internal/models"
)

var ErrConnectionInvalidated = errors.New("IMAP connection invalidated")
var ErrPoolClosed = errors.New("IMAP connection pool closed")

type poolEntry struct {
	accountID     uint
	identity      string
	configVersion uint64
	client        *Client
	busy          bool
	invalid       bool
	temporary     bool
	lastUsed      time.Time
	cancel        context.CancelFunc
}

// ConnectionPool is the single admission ledger for idle, leased, dialing and
// temporary connections. A slot is released only after its transport is closed.
type ConnectionPool struct {
	mu               sync.Mutex
	entries          map[*poolEntry]struct{}
	versions         map[uint]uint64
	changed          chan struct{}
	closeDone        chan struct{}
	closed           bool
	maxPerAccount    int
	maxTotal         int
	maxIdle          time.Duration
	healthCheckAfter time.Duration
	waitCount        uint64
	waitDuration     time.Duration
	dial             func(context.Context, *models.EmailAccount) (*Client, error)
}

// NewConnectionPool creates a pool owned by the application. Use the same instance for
// mailbox leases and account connection tests, and Close it during shutdown.
func NewConnectionPool() *ConnectionPool {
	p := &ConnectionPool{
		entries: make(map[*poolEntry]struct{}), versions: make(map[uint]uint64),
		changed: make(chan struct{}), closeDone: make(chan struct{}), maxPerAccount: 3, maxTotal: 16,
		maxIdle: 10 * time.Minute, healthCheckAfter: 30 * time.Second,
	}
	p.dial = p.dialWithRetryContext
	return p
}

func connectionIdentity(account *models.EmailAccount) string {
	if account.ID != 0 {
		return "id:" + strconv.FormatUint(uint64(account.ID), 10)
	}
	return "address:" + net.JoinHostPort(strings.ToLower(account.IMAPHost), strconv.Itoa(account.IMAPPort)) + "\x00" + account.Username
}

func (p *ConnectionPool) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// AcquireConnectionContext returns an exclusive lease. release is idempotent;
// any command error or cancelled command must be released with healthy=false.
func (p *ConnectionPool) AcquireConnectionContext(ctx context.Context, account *models.EmailAccount) (*Client, func(bool), error) {
	return p.acquire(ctx, account, false, p.dial)
}

func (p *ConnectionPool) acquire(ctx context.Context, account *models.EmailAccount, temporary bool, dial func(context.Context, *models.EmailAccount) (*Client, error)) (*Client, func(bool), error) {
	if account == nil {
		return nil, nil, errors.New("account is nil")
	}
	identity := connectionIdentity(account)
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, nil, ErrPoolClosed
		}
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, nil, err
		}
		if account.ID != 0 && account.ConfigVersion > p.versions[account.ID] {
			entries := p.invalidateLocked(account.ID, account.ConfigVersion)
			p.mu.Unlock()
			p.closeInvalidated(entries)
			continue
		}
		if account.ID != 0 && account.ConfigVersion < p.versions[account.ID] {
			p.mu.Unlock()
			return nil, nil, ErrConnectionInvalidated
		}
		count := 0
		var idle, evict *poolEntry
		for entry := range p.entries {
			if entry.identity == identity {
				count++
				if !entry.busy && !entry.invalid && !entry.temporary {
					idle = entry
				}
			}
			if !entry.busy && !entry.invalid && (evict == nil || entry.lastUsed.Before(evict.lastUsed)) {
				evict = entry
			}
		}
		if temporary && count >= p.maxPerAccount && idle != nil {
			evict = idle
		}
		// Expiration and capacity eviction use the same ledger, including temporary
		// requests. Keep the slot reserved until closing outside the mutex completes.
		if evict != nil && (time.Since(evict.lastUsed) > p.maxIdle ||
			((temporary || idle == nil) && (len(p.entries) >= p.maxTotal || count >= p.maxPerAccount) &&
				(count < p.maxPerAccount || evict.identity == identity))) {
			evict.invalid = true
			evict.busy = true
			p.mu.Unlock()
			evict.client.closeTransport()
			p.mu.Lock()
			delete(p.entries, evict)
			p.notifyLocked()
			p.mu.Unlock()
			continue
		}
		if !temporary && idle != nil {
			idle.busy = true
			activity := idle.client.lastActivity
			if activity.IsZero() {
				activity = idle.lastUsed
			}
			if time.Since(activity) < p.healthCheckAfter {
				p.mu.Unlock()
				return idle.client, p.releaseFunc(idle), nil
			}
			operationCtx, cancel := context.WithCancel(ctx)
			idle.cancel = cancel
			p.mu.Unlock()
			checkCtx, stopCheck := context.WithTimeout(operationCtx, 5*time.Second)
			err := idle.client.noopContext(checkCtx)
			stopCheck()
			client := idle.client
			if err != nil {
				client.closeTransport()
				if operationCtx.Err() == nil {
					client, err = dial(operationCtx, account)
				} else {
					err = operationCtx.Err()
				}
			}
			return p.completeAcquire(ctx, idle, client, err, cancel)
		}
		if count < p.maxPerAccount && len(p.entries) < p.maxTotal {
			operationCtx, cancel := context.WithCancel(ctx)
			entry := &poolEntry{accountID: account.ID, identity: identity, configVersion: account.ConfigVersion,
				busy: true, temporary: temporary, cancel: cancel}
			p.entries[entry] = struct{}{}
			p.mu.Unlock()
			client, err := dial(operationCtx, account)
			return p.completeAcquire(ctx, entry, client, err, cancel)
		}
		changed := p.changed
		waitStarted := time.Now()
		p.waitCount++
		p.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		p.mu.Lock()
		p.waitDuration += time.Since(waitStarted)
		p.mu.Unlock()
	}
}

func (p *ConnectionPool) completeAcquire(ctx context.Context, entry *poolEntry, client *Client, err error, cancel context.CancelFunc) (*Client, func(bool), error) {
	cancel()
	p.mu.Lock()
	entry.cancel = nil
	if p.closed {
		err = ErrPoolClosed
	} else if ctx.Err() != nil {
		err = ctx.Err()
	} else if entry.invalid {
		err = ErrConnectionInvalidated
	}
	if err != nil {
		entry.invalid = true
		p.mu.Unlock()
		if client != nil {
			client.closeTransport()
		}
		p.mu.Lock()
		delete(p.entries, entry)
		p.notifyLocked()
		p.mu.Unlock()
		return nil, nil, err
	}
	entry.client = client
	p.mu.Unlock()
	return client, p.releaseFunc(entry), nil
}

func (p *ConnectionPool) releaseFunc(entry *poolEntry) func(bool) {
	var once sync.Once
	return func(healthy bool) {
		once.Do(func() {
			p.mu.Lock()
			if _, exists := p.entries[entry]; !exists {
				p.mu.Unlock()
				return
			}
			if healthy && !entry.invalid && !entry.temporary && !p.closed {
				entry.busy = false
				entry.lastUsed = time.Now()
				p.notifyLocked()
				p.mu.Unlock()
				return
			}
			entry.invalid = true
			p.mu.Unlock()
			entry.client.closeTransport()
			p.mu.Lock()
			delete(p.entries, entry)
			p.notifyLocked()
			p.mu.Unlock()
		})
	}
}

// RemoveConnection invalidates older account snapshots without closing a lease
// already acquired using the newly committed configuration.
func (p *ConnectionPool) RemoveConnection(accountID uint, configVersion uint64) {
	p.mu.Lock()
	entries := p.invalidateLocked(accountID, configVersion)
	p.mu.Unlock()
	p.closeInvalidated(entries)
}

func (p *ConnectionPool) invalidateLocked(accountID uint, configVersion uint64) []*poolEntry {
	if configVersion > p.versions[accountID] {
		p.versions[accountID] = configVersion
	}
	var entries []*poolEntry
	for entry := range p.entries {
		if entry.accountID != accountID || entry.configVersion >= p.versions[accountID] {
			continue
		}
		entry.invalid = true
		if entry.cancel != nil {
			entry.cancel()
		}
		entries = append(entries, entry)
	}
	p.notifyLocked()
	return entries
}

func (p *ConnectionPool) closeInvalidated(entries []*poolEntry) {
	for _, entry := range entries {
		if entry.client != nil {
			entry.client.closeTransport()
		}
	}
	p.mu.Lock()
	for _, entry := range entries {
		if !entry.busy {
			delete(p.entries, entry)
		}
	}
	p.notifyLocked()
	p.mu.Unlock()
}

// Close prevents admission, wakes waiters, cancels in-flight setup/health checks
// and closes every established transport, including currently leased clients.
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	if p.closed {
		done := p.closeDone
		p.mu.Unlock()
		<-done
		return nil
	}
	p.closed = true
	var clients []*Client
	for entry := range p.entries {
		entry.invalid = true
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.client != nil {
			clients = append(clients, entry.client)
		}
	}
	p.notifyLocked()
	p.mu.Unlock()
	for _, client := range clients {
		client.closeTransport()
	}
	p.mu.Lock()
	for entry := range p.entries {
		if entry.client != nil && entry.cancel == nil {
			delete(p.entries, entry)
		}
	}
	p.notifyLocked()
	for len(p.entries) != 0 {
		changed := p.changed
		p.mu.Unlock()
		<-changed
		p.mu.Lock()
	}
	close(p.closeDone)
	p.mu.Unlock()
	return nil
}

// TestConnectionContext shares admission with mailbox operations but never
// retains its connection as idle, even when the test succeeds.
func (p *ConnectionPool) TestConnectionContext(ctx context.Context, account *models.EmailAccount) error {
	if account == nil || account.IMAPHost == "" || account.Username == "" || account.Password == "" {
		return errors.New("invalid configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()
	client, release, err := p.acquire(ctx, account, true, p.dial)
	if err != nil {
		return err
	}
	defer release(false)
	finish, err := client.commandContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	err = client.Disconnect()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (p *ConnectionPool) dialWithRetryContext(ctx context.Context, account *models.EmailAccount) (*Client, error) {
	var lastErr error
	const maxAttempts = 2
	for retry := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		client := NewClient(account, p)
		err := client.connectContext(attemptCtx)
		cancel()
		if err == nil {
			return client, nil
		}
		client.closeTransport()
		lastErr = err
		if IsAuthError(err) || ctx.Err() != nil {
			return nil, lastErr
		}
		if retry < maxAttempts-1 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, fmt.Errorf("failed to connect after %d attempts: %w", maxAttempts, lastErr)
}

func (p *ConnectionPool) GetStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	connections := make([]map[string]interface{}, 0, len(p.entries))
	temporary := 0
	for entry := range p.entries {
		if entry.temporary {
			temporary++
		}
		connections = append(connections, map[string]interface{}{
			"account_id": entry.accountID, "connected": entry.client != nil && !entry.invalid,
			"in_use": entry.busy, "last_used": entry.lastUsed, "temporary": entry.temporary,
		})
	}
	return map[string]interface{}{
		"total_connections": len(p.entries), "connections": connections, "temporary_connections": temporary,
		"wait_count": p.waitCount, "wait_duration_ms": p.waitDuration.Milliseconds(), "closed": p.closed,
	}
}
