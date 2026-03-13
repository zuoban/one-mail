package sync

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
)

type SyncCache struct {
	filters map[uint]*bloom.BloomFilter
	mu      sync.RWMutex
}

var globalSyncCache *SyncCache
var cacheOnce sync.Once

func GetSyncCache() *SyncCache {
	cacheOnce.Do(func() {
		globalSyncCache = &SyncCache{
			filters: make(map[uint]*bloom.BloomFilter),
		}
	})
	return globalSyncCache
}

func (c *SyncCache) GetOrCreateFilter(accountID uint, expectedItems uint) *bloom.BloomFilter {
	c.mu.RLock()
	filter, exists := c.filters[accountID]
	c.mu.RUnlock()

	if exists {
		return filter
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if filter, exists := c.filters[accountID]; exists {
		return filter
	}

	filter = bloom.NewWithEstimates(expectedItems, 0.01)
	c.filters[accountID] = filter
	return filter
}

func (c *SyncCache) MightContain(accountID uint, messageID string) bool {
	c.mu.RLock()
	filter, exists := c.filters[accountID]
	c.mu.RUnlock()

	if !exists {
		return false
	}

	return filter.TestString(messageID)
}

func (c *SyncCache) Add(accountID uint, messageID string) {
	c.mu.RLock()
	filter, exists := c.filters[accountID]
	c.mu.RUnlock()

	if !exists {
		filter = c.GetOrCreateFilter(accountID, 10000)
		c.mu.RLock()
		filter = c.filters[accountID]
		c.mu.RUnlock()
	}

	filter.AddString(messageID)
}

func (c *SyncCache) AddBatch(accountID uint, messageIDs []string) {
	if len(messageIDs) == 0 {
		return
	}

	filter := c.GetOrCreateFilter(accountID, uint(len(messageIDs)*10))

	for _, messageID := range messageIDs {
		filter.AddString(messageID)
	}
}

func (c *SyncCache) Clear(accountID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.filters, accountID)
}

func (c *SyncCache) Rebuild(accountID uint, messageIDs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(messageIDs) == 0 {
		delete(c.filters, accountID)
		return
	}

	filter := bloom.NewWithEstimates(uint(len(messageIDs)*2), 0.01)
	for _, messageID := range messageIDs {
		filter.AddString(messageID)
	}

	c.filters[accountID] = filter
}

func (c *SyncCache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"total_filters": len(c.filters),
		"filters":       make(map[uint]map[string]interface{}),
	}

	for accountID, filter := range c.filters {
		stats["filters"].(map[uint]map[string]interface{})[accountID] = map[string]interface{}{
			"capacity":         filter.Cap(),
			"approximate_size": filter.ApproximatedSize(),
		}
	}

	return stats
}
