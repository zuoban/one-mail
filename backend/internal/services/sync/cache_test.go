package sync

import (
	"testing"
)

func TestSyncCache_GetOrCreateFilter(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1001)
	filter := cache.GetOrCreateFilter(accountID, 1000)

	if filter == nil {
		t.Error("Filter should not be nil")
	}

	filter2 := cache.GetOrCreateFilter(accountID, 1000)
	if filter != filter2 {
		t.Error("Should return the same filter for the same account")
	}

	cache.Clear(accountID)
}

func TestSyncCache_MightContain(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1002)
	cache.Clear(accountID)

	messageID := "test-message-id-123"

	exists := cache.MightContain(accountID, messageID)
	if exists {
		t.Error("Should not contain message ID initially")
	}

	cache.Add(accountID, messageID)

	exists = cache.MightContain(accountID, messageID)
	if !exists {
		t.Error("Should contain message ID after adding")
	}

	cache.Clear(accountID)
}

func TestSyncCache_AddBatch(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1003)
	cache.Clear(accountID)

	messageIDs := []string{
		"message-1",
		"message-2",
		"message-3",
	}

	cache.AddBatch(accountID, messageIDs)

	for _, messageID := range messageIDs {
		if !cache.MightContain(accountID, messageID) {
			t.Errorf("Should contain message ID: %s", messageID)
		}
	}

	cache.Clear(accountID)
}

func TestSyncCache_Rebuild(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1004)
	cache.Clear(accountID)

	messageIDs := []string{
		"rebuild-message-1",
		"rebuild-message-2",
		"rebuild-message-3",
	}

	cache.Rebuild(accountID, messageIDs)

	for _, messageID := range messageIDs {
		if !cache.MightContain(accountID, messageID) {
			t.Errorf("Should contain message ID after rebuild: %s", messageID)
		}
	}

	cache.Clear(accountID)
}

func TestSyncCache_Clear(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1005)
	messageID := "clear-test-message"

	cache.Add(accountID, messageID)

	if !cache.MightContain(accountID, messageID) {
		t.Error("Should contain message ID")
	}

	cache.Clear(accountID)

	if cache.MightContain(accountID, messageID) {
		t.Error("Should not contain message ID after clear")
	}
}

func TestSyncCache_GetStats(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1006)
	cache.Clear(accountID)

	cache.Add(accountID, "test-message")

	stats := cache.GetStats()

	if stats["total_filters"] == nil {
		t.Error("Stats should contain total_filters")
	}

	if stats["filters"] == nil {
		t.Error("Stats should contain filters")
	}

	cache.Clear(accountID)
}

func TestSyncCache_ConcurrentAccess(t *testing.T) {
	cache := GetSyncCache()

	accountID := uint(1007)
	cache.Clear(accountID)

	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func(num int) {
			messageID := string(rune(num))
			cache.Add(accountID, messageID)
			_ = cache.MightContain(accountID, messageID)
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	cache.Clear(accountID)
}
