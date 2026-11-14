package imap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"one-mail/backend/internal/models"
)

var ErrConnectionInvalidated = errors.New("IMAP connection invalidated")

type poolEntry struct {
	accountID uint
	client    *Client
	busy      bool
	invalid   bool
	lastUsed  time.Time
	cancel    context.CancelFunc
}

// Counts include idle, leased and dialing connections. Invalid leases retain their
// slots until their owner releases them, so removal cannot exceed the limits.
type ConnectionPool struct {
	mu            sync.Mutex
	entries       map[*poolEntry]struct{}
	generation    map[uint]uint64
	changed       chan struct{}
	maxPerAccount int
	maxTotal      int
	maxIdle       time.Duration
	dial          func(context.Context, *models.EmailAccount) (*Client, error)
}

func newConnectionPool() *ConnectionPool {
	p := &ConnectionPool{
		entries: make(map[*poolEntry]struct{}), generation: make(map[uint]uint64),
		changed: make(chan struct{}), maxPerAccount: 3, maxTotal: 16, maxIdle: 10 * time.Minute,
	}
	p.dial = p.dialWithRetryContext
	return p
}

var globalPool *ConnectionPool
var poolOnce sync.Once

func GetConnectionPool() *ConnectionPool {
	poolOnce.Do(func() { globalPool = newConnectionPool() })
	return globalPool
}

func (p *ConnectionPool) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// AcquireConnectionContext returns an exclusive lease. release is idempotent;
// any command error or cancelled command must be released with healthy=false.
func (p *ConnectionPool) AcquireConnectionContext(ctx context.Context, account *models.EmailAccount) (*Client, func(bool), error) {
	if account == nil {
		return nil, nil, errors.New("account is nil")
	}
	p.mu.Lock()
	generation := p.generation[account.ID]
	for {
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, nil, err
		}
		if generation != p.generation[account.ID] {
			p.mu.Unlock()
			return nil, nil, ErrConnectionInvalidated
		}
		count := 0
		var idle *poolEntry
		for entry := range p.entries {
			if !entry.busy && time.Since(entry.lastUsed) > p.maxIdle {
				entry.client.ForceClose()
				delete(p.entries, entry)
				continue
			}
			if entry.accountID == account.ID {
				count++
				if !entry.busy && !entry.invalid {
					idle = entry
				}
			}
		}
		if idle != nil {
			idle.busy = true
			p.mu.Unlock()
			return idle.client, p.releaseFunc(idle), nil
		}
		// Evict another account's idle connection when all global slots are occupied.
		if count < p.maxPerAccount && len(p.entries) >= p.maxTotal {
			for entry := range p.entries {
				if !entry.busy {
					entry.client.ForceClose()
					delete(p.entries, entry)
					break
				}
			}
		}
		if count < p.maxPerAccount && len(p.entries) < p.maxTotal {
			dialCtx, cancel := context.WithCancel(ctx)
			entry := &poolEntry{accountID: account.ID, busy: true, cancel: cancel}
			p.entries[entry] = struct{}{}
			p.mu.Unlock()
			client, err := p.dial(dialCtx, account)
			cancel()
			p.mu.Lock()
			entry.cancel = nil
			if err == nil && (entry.invalid || ctx.Err() != nil) {
				client.ForceClose()
				err = ErrConnectionInvalidated
				if ctx.Err() != nil {
					err = ctx.Err()
				}
			}
			if err != nil {
				delete(p.entries, entry)
				p.notifyLocked()
				p.mu.Unlock()
				return nil, nil, err
			}
			entry.client = client
			p.mu.Unlock()
			return client, p.releaseFunc(entry), nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-changed:
		}
		p.mu.Lock()
	}
}

func (p *ConnectionPool) releaseFunc(entry *poolEntry) func(bool) {
	var once sync.Once
	return func(healthy bool) {
		once.Do(func() {
			p.mu.Lock()
			if !healthy || entry.invalid {
				entry.client.ForceClose()
				delete(p.entries, entry)
			} else {
				entry.busy = false
				entry.lastUsed = time.Now()
			}
			p.notifyLocked()
			p.mu.Unlock()
		})
	}
}

func (p *ConnectionPool) RemoveConnection(accountID uint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation[accountID]++
	for entry := range p.entries {
		if entry.accountID != accountID {
			continue
		}
		entry.invalid = true
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.client != nil {
			entry.client.ForceClose()
		}
		if !entry.busy {
			delete(p.entries, entry)
		}
	}
	p.notifyLocked()
}

func (p *ConnectionPool) dialWithRetryContext(ctx context.Context, account *models.EmailAccount) (*Client, error) {
	var lastErr error
	const maxAttempts = 2
	for retry := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		client := NewClient(account)
		err := client.ConnectContext(attemptCtx)
		cancel()
		if err == nil {
			return client, nil
		}
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
	for entry := range p.entries {
		connections = append(connections, map[string]interface{}{
			"account_id": entry.accountID, "connected": entry.client != nil && !entry.invalid,
			"in_use": entry.busy, "last_used": entry.lastUsed,
		})
	}
	return map[string]interface{}{"total_connections": len(p.entries), "connections": connections}
}
