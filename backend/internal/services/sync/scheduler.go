package sync

import (
	"context"
	"log"
	"sync"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

type Scheduler struct {
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	ticker       *time.Ticker
	running      bool
	interval     time.Duration
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

	s.interval = s.getDefaultInterval()
	if s.interval <= 0 {
		s.interval = 5 * time.Minute
	}

	s.running = true

	go s.run()
	go s.startLogCleanup()

	log.Printf("Sync scheduler started with interval: %v", s.interval)
}

func (s *Scheduler) getDefaultInterval() time.Duration {
	var user models.User
	if err := database.GetDB().First(&user).Error; err != nil {
		log.Printf("Failed to get user config, using default: %v", err)
		return 5 * time.Minute
	}
	return time.Duration(user.DefaultSyncInterval) * time.Minute
}

func (s *Scheduler) run() {
	// 等待到下一个整分钟
	waitDuration := s.calculateWaitToNextMinute()
	if waitDuration > 0 {
		log.Printf("First sync scheduled at: %v", time.Now().Add(waitDuration).Format("15:04:05"))
		time.Sleep(waitDuration)
	}

	s.syncAllAccounts()

	// 创建对齐到整分钟的 ticker
	ticker := s.createAlignedTicker()
	s.mu.Lock()
	s.ticker = ticker
	s.mu.Unlock()

	for {
		s.mu.RLock()
		ticker := s.ticker
		ctx := s.ctx
		trigger := s.trigger
		s.mu.RUnlock()

		select {
		case <-ctx.Done():
			return
		case <-trigger:
			s.syncAllAccounts()
		case <-ticker.C:
			s.syncAllAccounts()
		}
	}
}

func (s *Scheduler) calculateWaitToNextMinute() time.Duration {
	s.mu.RLock()
	interval := s.interval
	s.mu.RUnlock()

	if interval <= 0 {
		return 0
	}

	now := time.Now()
	minutes := int(interval.Minutes())

	var nextTick time.Time
	if minutes > 0 {
		// 计算下一个同步点（对齐到 interval 分钟）
		nextMinute := ((now.Minute() / minutes) + 1) * minutes
		nextTick = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())

		// 如果计算的时间已经过去，再加一个间隔
		if nextTick.Before(now) || nextTick.Equal(now) {
			nextTick = nextTick.Add(interval)
		}
	} else {
		// 间隔小于1分钟，对齐到下一分钟
		nextTick = now.Add(time.Minute)
		nextTick = time.Date(nextTick.Year(), nextTick.Month(), nextTick.Day(), nextTick.Hour(), nextTick.Minute(), 0, 0, nextTick.Location())
	}

	return time.Until(nextTick)
}

func (s *Scheduler) createAlignedTicker() *time.Ticker {
	s.mu.RLock()
	interval := s.interval
	s.mu.RUnlock()

	if interval <= 0 {
		return time.NewTicker(time.Minute)
	}

	return time.NewTicker(interval)
}

func (s *Scheduler) syncAllAccounts() {
	var accounts []models.EmailAccount
	if err := database.GetDB().Where("enable_auto_sync = ? AND status = ?", true, "active").Find(&accounts).Error; err != nil {
		log.Printf("Failed to load accounts: %v", err)
		return
	}

	if len(accounts) == 0 {
		log.Println("No accounts to sync")
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
				// 正常完成
			case <-time.After(5 * time.Minute):
				// 超时，强制重置状态
				log.Printf("Sync timeout for account %d (%s), force reset status", acc.ID, acc.Email)
				s.forceResetAccountStatus(acc.ID)
			}
		}(account)
	}
	wg.Wait()

	s.mu.Lock()
	s.lastSyncTime = time.Now()
	s.mu.Unlock()

	log.Printf("Completed sync for all accounts, next sync in %v", s.interval)
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
		if !status.StartTime.IsZero() && time.Since(status.StartTime) > 10*time.Minute {
			log.Printf("Force reset stuck account %d (running for %v)", accountID, time.Since(status.StartTime))
			status.Running = false
		} else {
			status.mu.Unlock()
			log.Printf("Skip sync for account %d: already running", accountID)
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
}

func (s *Scheduler) startLogCleanup() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOldLogs()
		}
	}
}

func (s *Scheduler) cleanupOldLogs() {
	cutoff := time.Now().AddDate(0, 0, -2)
	result := database.GetDB().Where("created_at < ?", cutoff).Delete(&models.SyncLog{})
	if result.Error != nil {
		log.Printf("Failed to cleanup old sync logs: %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d old sync logs", result.RowsAffected)
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	if s.ticker != nil {
		s.ticker.Stop()
	}
	s.running = false
	s.statuses = make(map[uint]*SyncStatus)

	log.Println("Sync scheduler stopped")
}

func (s *Scheduler) UpdateInterval(interval time.Duration) {
	s.mu.Lock()
	s.interval = interval

	if s.running {
		if s.ticker != nil {
			s.ticker.Stop()
		}
		s.ticker = s.createAlignedTicker()
		s.mu.Unlock()

		log.Printf("Sync interval updated to: %v, next sync at: %v", interval, s.GetNextSyncTime().Format("15:04:05"))
	} else {
		s.mu.Unlock()
	}

	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

func (s *Scheduler) TriggerSync(accountID uint) error {
	s.syncAccount(accountID)
	return nil
}

func (s *Scheduler) TriggerAllSync() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
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

func (s *Scheduler) GetInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interval
}

func (s *Scheduler) GetLastSyncTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSyncTime
}

func (s *Scheduler) GetNextSyncTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running || s.interval <= 0 {
		return time.Time{}
	}

	now := time.Now()
	minutes := int(s.interval.Minutes())

	var nextTick time.Time
	if minutes > 0 {
		nextMinute := ((now.Minute() / minutes) + 1) * minutes
		nextTick = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), nextMinute, 0, 0, now.Location())

		if nextTick.Before(now) || nextTick.Equal(now) {
			nextTick = nextTick.Add(s.interval)
		}
	} else {
		nextTick = now.Add(time.Minute)
		nextTick = time.Date(nextTick.Year(), nextTick.Month(), nextTick.Day(), nextTick.Hour(), nextTick.Minute(), 0, 0, nextTick.Location())
	}

	return nextTick
}
