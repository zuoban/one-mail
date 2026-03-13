package imap

import (
	"sync"
	"testing"
	"time"

	"one-mail/backend/internal/models"
)

func TestConnectionPool_GetConnection(t *testing.T) {
	pool := GetConnectionPool()

	account := &models.EmailAccount{
		IMAPHost: "imap.gmail.com",
		IMAPPort: 993,
	}

	pool.RemoveConnection(account.ID)

	_, err := pool.GetConnection(account)
	if err == nil {
		t.Log("Connection successful (requires valid credentials)")
		pool.RemoveConnection(account.ID)
	} else {
		t.Logf("Connection failed as expected (no credentials): %v", err)
	}
}

func TestConnectionPool_ConcurrentAccess(t *testing.T) {
	pool := GetConnectionPool()

	account := &models.EmailAccount{
		ID:       999,
		IMAPHost: "imap.test.com",
		IMAPPort: 993,
	}

	pool.RemoveConnection(account.ID)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pool.GetConnection(account)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Logf("Expected error: %v", err)
		errorCount++
	}

	if errorCount != 10 {
		t.Errorf("Expected 10 errors, got %d", errorCount)
	}

	pool.RemoveConnection(account.ID)
}

func TestConnectionPool_ReleaseConnection(t *testing.T) {
	pool := GetConnectionPool()

	account := &models.EmailAccount{
		ID:       1001,
		IMAPHost: "imap.test.com",
		IMAPPort: 993,
	}

	pool.RemoveConnection(account.ID)

	pool.ReleaseConnection(account.ID)

	pool.mu.RLock()
	_, exists := pool.connections[account.ID]
	pool.mu.RUnlock()

	if exists {
		t.Error("Connection should not exist after release of non-existent connection")
	}
}

func TestConnectionPool_Cleanup(t *testing.T) {
	pool := GetConnectionPool()
	pool.maxIdle = 100 * time.Millisecond

	account := &models.EmailAccount{
		ID:       1002,
		IMAPHost: "imap.test.com",
		IMAPPort: 993,
	}

	pool.RemoveConnection(account.ID)

	wrapper := &ConnectionWrapper{
		client:    nil,
		lastUsed:  time.Now().Add(-200 * time.Millisecond),
		accountID: account.ID,
	}

	pool.mu.Lock()
	pool.connections[account.ID] = wrapper
	pool.mu.Unlock()

	time.Sleep(150 * time.Millisecond)
	pool.cleanup()

	pool.mu.RLock()
	_, exists := pool.connections[account.ID]
	pool.mu.RUnlock()

	if exists {
		t.Error("Connection should be cleaned up")
	}

	pool.maxIdle = 10 * time.Minute
}

func TestConnectionPool_GetStats(t *testing.T) {
	pool := GetConnectionPool()

	stats := pool.GetStats()

	if stats["total_connections"] == nil {
		t.Error("Stats should contain total_connections")
	}

	if stats["connections"] == nil {
		t.Error("Stats should contain connections")
	}
}

func TestConnectionPool_HealthCheck(t *testing.T) {
	pool := GetConnectionPool()

	account := &models.EmailAccount{
		ID:       1003,
		IMAPHost: "imap.test.com",
		IMAPPort: 993,
	}

	pool.RemoveConnection(account.ID)

	err := pool.HealthCheck(account.ID)
	if err == nil {
		t.Error("HealthCheck should fail for non-existent connection")
	}

	wrapper := &ConnectionWrapper{
		client:    nil,
		lastUsed:  time.Now(),
		accountID: account.ID,
	}

	pool.mu.Lock()
	pool.connections[account.ID] = wrapper
	pool.mu.Unlock()

	err = pool.HealthCheck(account.ID)
	if err == nil {
		t.Error("HealthCheck should fail for nil client")
	}

	pool.RemoveConnection(account.ID)
}
