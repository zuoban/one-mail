package sync

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

type Scheduler struct {
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	cron         *cron.Cron
	running      bool
	statuses     map[uint]*SyncStatus
	trigger      chan struct{}
	lastSyncTime time.Time
}

type SyncStatus struct {
	AccountID    uint      `json:"account_id"`
	Running      bool      `json:"running"`
	StartTime    time.Time `json:"start_time"`
	LastSyncTime time.Time `json:"last_sync_time"`
	Error        string    `json:"error"`
	NewCount     int       `json:"new_count"`
	mu           sync.RWMutex
}

var globalScheduler *Scheduler
var once sync.Once

func GetScheduler() *Scheduler {
	once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalScheduler = &Scheduler{
			ctx:      ctx,
			cancel:   cancel,
			statuses: make(map[uint]*SyncStatus),
			trigger:  make(chan struct{}, 1),
		}
	})
	return globalScheduler
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.cron = cron.New()
	_, err := s.cron.AddFunc("@every 1m", func() {
		s.triggerSyncAll()
	})
	if err != nil {
		log.Printf("Failed to add cron job: %v", err)
		return
	}

	s.cron.Start()
	s.running = true

	go s.startTriggerListener()

	log.Println("Sync scheduler started with cron: @every 1m")
}

func (s *Scheduler) startTriggerListener() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.trigger:
			s.syncAllAccounts()
		}
	}
}

func (s *Scheduler) triggerSyncAll() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

func (s *Scheduler) syncAllAccounts() {
	var accounts []models.EmailAccount
	if err := database.GetDB().Where("enable_auto_sync = ? AND status = ?", true, "active").Find(&accounts).Error; err != nil {
		log.Printf("Failed to load accounts: %v", err)
		return
	}

	if len(accounts) == 0 {
		return
	}

	log.Printf("Starting sync for %d accounts", len(accounts))

	var wg sync.WaitGroup
	for _, account := range accounts {
		wg.Add(1)
		go func(acc models.EmailAccount) {
			defer wg.Done()

			done := make(chan struct{})
			go func() {
				s.syncAccount(acc.ID)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(5 * time.Minute):
				log.Printf("Sync timeout for account %d (%s), force reset status", acc.ID, acc.Email)
				s.forceResetAccountStatus(acc.ID)
			}
		}(account)
	}
	wg.Wait()

	s.mu.Lock()
	s.lastSyncTime = time.Now()
	s.mu.Unlock()

	log.Printf("Completed sync for all accounts")
}

func (s *Scheduler) forceResetAccountStatus(accountID uint) {
	s.mu.Lock()
	status, exists := s.statuses[accountID]
	if !exists {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	status.mu.Lock()
	status.Running = false
	status.StartTime = time.Time{}
	status.Error = "sync timeout after 5 minutes"
	status.mu.Unlock()

	if err := database.GetDB().Model(&models.SyncState{}).
		Where("account_id = ? AND is_syncing = ?", accountID, true).
		Updates(map[string]interface{}{ "is_syncing": false, "error": "sync timeout reset"}).Error; err != nil {
		log.Printf("Failed to reset sync_state for account %d: %v", accountID, err)
	}

	log.Printf("Account %d status force reset due to timeout", accountID)
}

func (s *Scheduler) syncAccount(accountID uint) {
	s.mu.Lock()
	status, exists := s.statuses[accountID]
	if !exists {
		status = &SyncStatus{
			AccountID: accountID,
			Running:   false,
		}
		s.statuses[accountID] = status
	}
	s.mu.Unlock()

	status.mu.Lock()
	if status.Running {
		runningFor := time.Since(status.StartTime)
		if !status.StartTime.IsZero() && runningFor > 10*time.Minute {
			log.Printf("Force reset stuck account %d (running for %v)", accountID, runningFor)
			status.Running = false
		} else {
			status.mu.Unlock()
			log.Printf("Skip sync for account %d: already running (running_for=%v)", accountID, runningFor)
			return
		}
	}
	status.Running = true
	status.StartTime = time.Now()
	status.Error = ""
	status.mu.Unlock()

	startTime := time.Now()
	syncLog := models.SyncLog{
		AccountID: accountID,
		StartTime: startTime,
		Status:    "running",
	}
	database.GetDB().Create(&syncLog)

	defer func() {
		status.mu.Lock()
		status.Running = false
		status.LastSyncTime = time.Now()
		status.StartTime = time.Time{}
		status.mu.Unlock()
	}()

	newCount, err := SyncAccount(accountID)

	endTime := time.Now()
	durationMs := endTime.Sub(startTime).Milliseconds()

	status.mu.Lock()
	if err != nil {
		status.Error = err.Error()
		log.Printf("Sync failed for account %d: %v", accountID, err)
		database.GetDB().Model(&syncLog).Updates(map[string]interface{}{
			"end_time":    endTime,
			"status":      "failed",
			"error":       err.Error(),
			"duration_ms": durationMs,
		})
	} else {
		status.LastSyncTime = time.Now()
		status.NewCount = newCount
		database.GetDB().Model(&syncLog).Updates(map[string]interface{}{
			"end_time":    endTime,
			"status":      "success",
			"new_count":   newCount,
			"duration_ms": durationMs,
		})
	}
	status.mu.Unlock()

	s.cleanupOldLogsForAccount(accountID)
}

func (s *Scheduler) cleanupOldLogsForAccount(accountID uint) {
	var count int64
	database.GetDB().Model(&models.SyncLog{}).Where("account_id = ?", accountID).Count(&count)

	if count > 20 {
		var logIDs []uint
		database.GetDB().Model(&models.SyncLog{}).
			Where("account_id = ?", accountID).
			Order("created_at DESC").
			Offset(20).
			Pluck("id", &logIDs)

		if len(logIDs) > 0 {
			result := database.GetDB().Where("id IN ?", logIDs).Delete(&models.SyncLog{})
			if result.Error != nil {
				log.Printf("Failed to cleanup old sync logs for account %d: %v", accountID, result.Error)
			} else {
				log.Printf("Cleaned up %d old sync logs for account %d", result.RowsAffected, accountID)
			}
		}
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	if s.cron != nil {
		s.cron.Stop()
	}
	s.running = false
	s.statuses = make(map[uint]*SyncStatus)

	log.Println("Sync scheduler stopped")
}

func (s *Scheduler) TriggerSync(accountID uint) error {
	s.syncAccount(accountID)
	return nil
}

func (s *Scheduler) TriggerAllSync() {
	s.triggerSyncAll()
}

func (s *Scheduler) GetStatus(accountID uint) *SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if status, exists := s.statuses[accountID]; exists {
		return status
	}

	return &SyncStatus{
		AccountID: accountID,
		Running:   false,
	}
}

func (s *Scheduler) GetAllStatuses() map[uint]*SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make(map[uint]*SyncStatus)
	for id, status := range s.statuses {
		statuses[id] = status
	}
	return statuses
}

func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Scheduler) GetLastSyncTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSyncTime
}
