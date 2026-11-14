package imap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"one-mail/backend/internal/models"
)

type ConnectionPool struct {
	connections map[uint]*ConnectionWrapper
	extraIdle   map[uint][]*ConnectionWrapper
	mu          sync.RWMutex
	maxIdle     time.Duration
	cleanupTick time.Duration
}

type ConnectionWrapper struct {
	client    *Client
	lastUsed  time.Time
	accountID uint
	inUse     bool
	mu        sync.Mutex
}

var globalPool *ConnectionPool
var poolOnce sync.Once

func GetConnectionPool() *ConnectionPool {
	poolOnce.Do(func() {
		globalPool = &ConnectionPool{
			connections: make(map[uint]*ConnectionWrapper),
			extraIdle:   make(map[uint][]*ConnectionWrapper),
			maxIdle:     10 * time.Minute,
			cleanupTick: 1 * time.Minute,
		}
		go globalPool.startCleanup()
	})
	return globalPool
}

// AcquireConnection leases an exclusive IMAP client for the given account.
// Callers MUST invoke the returned release(healthy) function when done; passing healthy=false
// closes and discards the connection (e.g. after timeout or network error).
func (p *ConnectionPool) AcquireConnection(account *models.EmailAccount) (*Client, func(healthy bool), error) {
	return p.AcquireConnectionContext(context.Background(), account)
}

func (p *ConnectionPool) AcquireConnectionContext(ctx context.Context, account *models.EmailAccount) (*Client, func(healthy bool), error) {
	// 1. Try primary slot
	p.mu.Lock()
	wrapper, exists := p.connections[account.ID]
	if exists && !wrapper.inUse {
		wrapper.inUse = true
		p.mu.Unlock()

		wrapper.mu.Lock()
		client := wrapper.client
		wrapper.mu.Unlock()

		if client != nil && client.IsConnected() {
			return client, func(healthy bool) {
				p.releasePrimary(account.ID, wrapper, client, healthy)
			}, nil
		}
		if client != nil {
			client.ForceClose()
		}
		newClient, err := p.dialWithRetryContext(ctx, account)
		if err != nil {
			p.mu.Lock()
			wrapper.inUse = false
			wrapper.mu.Lock()
			wrapper.client = nil
			wrapper.mu.Unlock()
			if cur, ok := p.connections[account.ID]; ok && cur == wrapper {
				delete(p.connections, account.ID)
			}
			p.mu.Unlock()
			return nil, nil, err
		}
		wrapper.mu.Lock()
		wrapper.client = newClient
		wrapper.lastUsed = time.Now()
		wrapper.mu.Unlock()
		return newClient, func(healthy bool) {
			p.releasePrimary(account.ID, wrapper, newClient, healthy)
		}, nil
	}

	// 2. Primary slot missing -> reserve it immediately so concurrent callers don't race
	if !exists {
		wrapper = &ConnectionWrapper{
			accountID: account.ID,
			lastUsed:  time.Now(),
			inUse:     true,
		}
		p.connections[account.ID] = wrapper
		p.mu.Unlock()

		newClient, err := p.dialWithRetryContext(ctx, account)
		if err != nil {
			p.mu.Lock()
			if cur, ok := p.connections[account.ID]; ok && cur == wrapper {
				delete(p.connections, account.ID)
			}
			p.mu.Unlock()
			return nil, nil, err
		}
		wrapper.mu.Lock()
		wrapper.client = newClient
		wrapper.lastUsed = time.Now()
		wrapper.mu.Unlock()
		return newClient, func(healthy bool) {
			p.releasePrimary(account.ID, wrapper, newClient, healthy)
		}, nil
	}

	// 3. Primary slot is currently in use -> pop from extraIdle if available
	extras := p.extraIdle[account.ID]
	if len(extras) > 0 {
		extra := extras[len(extras)-1]
		p.extraIdle[account.ID] = extras[:len(extras)-1]
		p.mu.Unlock()

		if extra.client != nil && extra.client.IsConnected() {
			return extra.client, func(healthy bool) {
				p.releaseExtra(account.ID, extra.client, healthy)
			}, nil
		}
		if extra.client != nil {
			extra.client.ForceClose()
		}
	} else {
		p.mu.Unlock()
	}

	// 4. Dial overflow connection
	newClient, err := p.dialWithRetryContext(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	return newClient, func(healthy bool) {
		p.releaseExtra(account.ID, newClient, healthy)
	}, nil
}

func (p *ConnectionPool) releasePrimary(accountID uint, wrapper *ConnectionWrapper, client *Client, healthy bool) {
	if !healthy {
		client.ForceClose()
		p.mu.Lock()
		wrapper.mu.Lock()
		if wrapper.client == client {
			wrapper.client = nil
		}
		wrapper.inUse = false
		wrapper.mu.Unlock()
		if cur, ok := p.connections[accountID]; ok && cur == wrapper && wrapper.client == nil {
			delete(p.connections, accountID)
		}
		p.mu.Unlock()
		return
	}

	wrapper.mu.Lock()
	wrapper.lastUsed = time.Now()
	wrapper.mu.Unlock()

	p.mu.Lock()
	wrapper.inUse = false
	p.mu.Unlock()
}

func (p *ConnectionPool) releaseExtra(accountID uint, client *Client, healthy bool) {
	if !healthy {
		client.ForceClose()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// If primary slot became empty, promote into primary
	if wrapper, exists := p.connections[accountID]; !exists {
		p.connections[accountID] = &ConnectionWrapper{
			client:    client,
			lastUsed:  time.Now(),
			accountID: accountID,
			inUse:     false,
		}
		return
	} else if !wrapper.inUse && wrapper.client == nil {
		wrapper.mu.Lock()
		wrapper.client = client
		wrapper.lastUsed = time.Now()
		wrapper.mu.Unlock()
		return
	}

	const maxExtraIdle = 2
	if len(p.extraIdle[accountID]) >= maxExtraIdle {
		go client.Disconnect()
		return
	}
	p.extraIdle[accountID] = append(p.extraIdle[accountID], &ConnectionWrapper{
		client:    client,
		lastUsed:  time.Now(),
		accountID: accountID,
	})
}

func (p *ConnectionPool) dialWithRetry(account *models.EmailAccount) (*Client, error) {
	return p.dialWithRetryContext(context.Background(), account)
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
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	return nil, fmt.Errorf("failed to connect after %d attempts: %w", maxAttempts, lastErr)
}

func (p *ConnectionPool) GetConnection(account *models.EmailAccount) (*Client, error) {
	p.mu.RLock()
	wrapper, exists := p.connections[account.ID]
	p.mu.RUnlock()

	if exists {
		wrapper.mu.Lock()
		defer wrapper.mu.Unlock()

		if wrapper.client != nil {
			if wrapper.client.IsConnected() {
				wrapper.lastUsed = time.Now()
				return wrapper.client, nil
			}
			_ = wrapper.client.Disconnect()
			wrapper.client = nil
		}
	}

	return p.createConnection(account)
}

func (p *ConnectionPool) createConnection(account *models.EmailAccount) (*Client, error) {
	client, err := p.dialWithRetry(account)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if old, exists := p.connections[account.ID]; exists && old.client != nil && !old.inUse {
		old.client.ForceClose()
	}
	p.connections[account.ID] = &ConnectionWrapper{
		client:    client,
		lastUsed:  time.Now(),
		accountID: account.ID,
	}
	p.mu.Unlock()

	return client, nil
}

func (p *ConnectionPool) ReleaseConnection(accountID uint) {
	p.mu.RLock()
	wrapper, exists := p.connections[accountID]
	p.mu.RUnlock()

	if !exists {
		return
	}

	wrapper.mu.Lock()
	wrapper.lastUsed = time.Now()
	wrapper.mu.Unlock()
}

func (p *ConnectionPool) RemoveConnection(accountID uint) {
	var toDisconnect []*Client

	p.mu.Lock()
	if wrapper, exists := p.connections[accountID]; exists {
		wrapper.mu.Lock()
		if wrapper.client != nil {
			toDisconnect = append(toDisconnect, wrapper.client)
			wrapper.client = nil
		}
		wrapper.mu.Unlock()
		delete(p.connections, accountID)
	}
	if extras, exists := p.extraIdle[accountID]; exists {
		for _, extra := range extras {
			if extra.client != nil {
				toDisconnect = append(toDisconnect, extra.client)
			}
		}
		delete(p.extraIdle, accountID)
	}
	p.mu.Unlock()

	for _, client := range toDisconnect {
		_ = client.Disconnect()
	}
}

func (p *ConnectionPool) startCleanup() {
	ticker := time.NewTicker(p.cleanupTick)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *ConnectionPool) cleanup() {
	var toDisconnect []*Client
	now := time.Now()

	p.mu.Lock()
	for id, wrapper := range p.connections {
		if wrapper.inUse {
			continue
		}
		wrapper.mu.Lock()
		if now.Sub(wrapper.lastUsed) > p.maxIdle {
			if wrapper.client != nil {
				toDisconnect = append(toDisconnect, wrapper.client)
				wrapper.client = nil
			}
			delete(p.connections, id)
		}
		wrapper.mu.Unlock()
	}
	for id, extras := range p.extraIdle {
		kept := extras[:0]
		for _, extra := range extras {
			if now.Sub(extra.lastUsed) > p.maxIdle {
				if extra.client != nil {
					toDisconnect = append(toDisconnect, extra.client)
				}
			} else {
				kept = append(kept, extra)
			}
		}
		if len(kept) == 0 {
			delete(p.extraIdle, id)
		} else {
			p.extraIdle[id] = kept
		}
	}
	p.mu.Unlock()

	for _, client := range toDisconnect {
		_ = client.Disconnect()
	}
}
func (p *ConnectionPool) HealthCheck(accountID uint) error {
	p.mu.RLock()
	wrapper, exists := p.connections[accountID]
	p.mu.RUnlock()

	if !exists {
		return fmt.Errorf("connection not found for account %d", accountID)
	}

	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()

	if wrapper.client == nil {
		return fmt.Errorf("client is nil")
	}

	if !wrapper.client.IsConnected() {
		return fmt.Errorf("client not connected")
	}

	return nil
}

func (p *ConnectionPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"total_connections": len(p.connections),
		"connections":       make([]map[string]interface{}, 0),
	}

	for id, wrapper := range p.connections {
		wrapper.mu.Lock()
		connInfo := map[string]interface{}{
			"account_id": id,
			"connected":  wrapper.client != nil && wrapper.client.IsConnected(),
			"last_used":  wrapper.lastUsed,
		}
		wrapper.mu.Unlock()
		stats["connections"] = append(stats["connections"].([]map[string]interface{}), connInfo)
	}

	return stats
}
