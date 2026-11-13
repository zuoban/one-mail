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
	defer c.mu.RUnlock()

	filter, exists := c.filters[accountID]
	if !exists {
		return false
	}

	return filter.TestString(messageID)
}

func (c *SyncCache) Add(accountID uint, messageID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	filter, exists := c.filters[accountID]
	if !exists {
		filter = bloom.NewWithEstimates(10000, 0.01)
		c.filters[accountID] = filter
	}

	filter.AddString(messageID)
}

func (c *SyncCache) AddBatch(accountID uint, messageIDs []string) {
	if len(messageIDs) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	filter, exists := c.filters[accountID]
	if !exists {
		filter = bloom.NewWithEstimates(uint(len(messageIDs)*10), 0.01)
		c.filters[accountID] = filter
	}

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

// NewDetachedFilter allocates an isolated bloom filter to populate off-lock before SwapFilter.
func NewDetachedFilter(expectedItems uint) *bloom.BloomFilter {
	if expectedItems < 1000 {
		expectedItems = 1000
	}
	return bloom.NewWithEstimates(expectedItems*2, 0.01)
}

// SwapFilter atomically replaces the bloom filter for an account under write lock.
func (c *SyncCache) SwapFilter(accountID uint, filter *bloom.BloomFilter) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if filter == nil {
		delete(c.filters, accountID)
		return
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
