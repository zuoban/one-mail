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
	mu      sync.RWMutex
	workers map[uint]*Worker
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
}

var globalScheduler *Scheduler
var once sync.Once

func GetScheduler() *Scheduler {
	once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalScheduler = &Scheduler{
			workers: make(map[uint]*Worker),
			ctx:     ctx,
			cancel:  cancel,
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

	s.running = true

	var accounts []models.EmailAccount
	if err := database.GetDB().Where("enable_auto_sync = ? AND status = ?", true, "active").Find(&accounts).Error; err != nil {
		log.Printf("Failed to load accounts for sync: %v", err)
		return
	}

	for _, account := range accounts {
		if account.SyncInterval > 0 {
			worker := NewWorker(account.ID, time.Duration(account.SyncInterval)*time.Minute)
			s.workers[account.ID] = worker
			go worker.Run(s.ctx)
		}
	}

	log.Printf("Sync scheduler started with %d workers", len(s.workers))
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.cancel()
	s.running = false

	for _, worker := range s.workers {
		worker.Stop()
	}

	s.workers = make(map[uint]*Worker)
	log.Println("Sync scheduler stopped")
}

func (s *Scheduler) AddAccount(accountID uint, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if worker, exists := s.workers[accountID]; exists {
		worker.UpdateInterval(interval)
		return
	}

	worker := NewWorker(accountID, interval)
	s.workers[accountID] = worker

	if s.running {
		go worker.Run(s.ctx)
	}
}

func (s *Scheduler) RemoveAccount(accountID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if worker, exists := s.workers[accountID]; exists {
		worker.Stop()
		delete(s.workers, accountID)
	}
}

func (s *Scheduler) TriggerSync(accountID uint) error {
	s.mu.RLock()
	worker, exists := s.workers[accountID]
	s.mu.RUnlock()

	if exists {
		return worker.TriggerSync()
	}

	return SyncAccount(accountID)
}

func (s *Scheduler) GetStatus(accountID uint) *SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if worker, exists := s.workers[accountID]; exists {
		return worker.GetStatus()
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
	for id, worker := range s.workers {
		statuses[id] = worker.GetStatus()
	}
	return statuses
}

func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
