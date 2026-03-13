package imap

import (
	"fmt"
	"sync"
	"time"

	"one-mail/backend/internal/models"
)

type ConnectionPool struct {
	connections map[uint]*ConnectionWrapper
	mu          sync.RWMutex
	maxIdle     time.Duration
	cleanupTick time.Duration
}

type ConnectionWrapper struct {
	client    *Client
	lastUsed  time.Time
	accountID uint
	mu        sync.Mutex
}

var globalPool *ConnectionPool
var poolOnce sync.Once

func GetConnectionPool() *ConnectionPool {
	poolOnce.Do(func() {
		globalPool = &ConnectionPool{
			connections: make(map[uint]*ConnectionWrapper),
			maxIdle:     10 * time.Minute,
			cleanupTick: 1 * time.Minute,
		}
		go globalPool.startCleanup()
	})
	return globalPool
}

func (p *ConnectionPool) GetConnection(account *models.EmailAccount) (*Client, error) {
	p.mu.RLock()
	wrapper, exists := p.connections[account.ID]
	p.mu.RUnlock()

	if exists {
		wrapper.mu.Lock()
		defer wrapper.mu.Unlock()

		if wrapper.client != nil && wrapper.client.IsConnected() {
			wrapper.lastUsed = time.Now()
			return wrapper.client, nil
		}

		_ = wrapper.client.Disconnect()
		wrapper.client = nil
	}

	return p.createConnection(account)
}

func (p *ConnectionPool) createConnection(account *models.EmailAccount) (*Client, error) {
	client := NewClient(account)

	var lastErr error
	for retry := 0; retry < 3; retry++ {
		if err := client.Connect(); err != nil {
			lastErr = err
			if retry < 2 {
				time.Sleep(time.Second * time.Duration(1<<retry))
			}
			continue
		}

		p.mu.Lock()
		p.connections[account.ID] = &ConnectionWrapper{
			client:    client,
			lastUsed:  time.Now(),
			accountID: account.ID,
		}
		p.mu.Unlock()

		return client, nil
	}

	return nil, fmt.Errorf("failed to connect after 3 retries: %w", lastErr)
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
	p.mu.Lock()
	defer p.mu.Unlock()

	if wrapper, exists := p.connections[accountID]; exists {
		wrapper.mu.Lock()
		if wrapper.client != nil {
			_ = wrapper.client.Disconnect()
		}
		wrapper.mu.Unlock()
		delete(p.connections, accountID)
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
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for id, wrapper := range p.connections {
		wrapper.mu.Lock()
		if now.Sub(wrapper.lastUsed) > p.maxIdle {
			if wrapper.client != nil {
				_ = wrapper.client.Disconnect()
			}
			delete(p.connections, id)
		}
		wrapper.mu.Unlock()
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
