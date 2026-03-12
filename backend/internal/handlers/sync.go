package handlers

import (
	"fmt"
	"net/http"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/sync"

	"github.com/gin-gonic/gin"
)

type SyncHandler struct{}

func NewSyncHandler() *SyncHandler {
	return &SyncHandler{}
}

type SyncStatusResponse struct {
	AccountID    uint      `json:"account_id"`
	Running      bool      `json:"running"`
	LastSyncTime time.Time `json:"last_sync_time"`
	Error        string    `json:"error"`
}

func (h *SyncHandler) GetStatus(c *gin.Context) {
	accountID := c.Param("id")
	userID := c.GetUint("user_id")

	var account models.EmailAccount
	if err := database.GetDB().
		Where("id = ? AND user_id = ?", accountID, userID).
		First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	status := sync.GetScheduler().GetStatus(account.ID)

	c.JSON(http.StatusOK, gin.H{"data": status})
}

func (h *SyncHandler) GetAllStatuses(c *gin.Context) {
	statuses := sync.GetScheduler().GetAllStatuses()
	result := make(map[string]*sync.SyncStatus)
	for id, status := range statuses {
		result[fmt.Sprintf("%d", id)] = status
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

type SyncConfigRequest struct {
	SyncInterval   int    `json:"sync_interval"`
	SyncFolders    string `json:"sync_folders"`
	EnableAutoSync bool   `json:"enable_auto_sync"`
}

func (h *SyncHandler) UpdateConfig(c *gin.Context) {
	accountID := c.Param("id")
	userID := c.GetUint("user_id")

	var account models.EmailAccount
	if err := database.GetDB().
		Where("id = ? AND user_id = ?", accountID, userID).
		First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	var req SyncConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{
		"sync_interval":    req.SyncInterval,
		"sync_folders":     req.SyncFolders,
		"enable_auto_sync": req.EnableAutoSync,
	}

	if err := database.GetDB().Model(&account).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	scheduler := sync.GetScheduler()
	if req.EnableAutoSync && req.SyncInterval > 0 {
		scheduler.AddAccount(account.ID, time.Duration(req.SyncInterval)*time.Minute)
	} else {
		scheduler.RemoveAccount(account.ID)
	}

	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}

func (h *SyncHandler) TriggerSync(c *gin.Context) {
	accountID := c.Param("id")
	userID := c.GetUint("user_id")

	var account models.EmailAccount
	if err := database.GetDB().
		Where("id = ? AND user_id = ?", accountID, userID).
		First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	if err := sync.GetScheduler().TriggerSync(account.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "sync triggered"})
}

func (h *SyncHandler) StartScheduler(c *gin.Context) {
	scheduler := sync.GetScheduler()
	if scheduler.IsRunning() {
		c.JSON(http.StatusOK, gin.H{"message": "scheduler already running"})
		return
	}

	scheduler.Start()
	c.JSON(http.StatusOK, gin.H{"message": "scheduler started"})
}

func (h *SyncHandler) StopScheduler(c *gin.Context) {
	scheduler := sync.GetScheduler()
	scheduler.Stop()
	c.JSON(http.StatusOK, gin.H{"message": "scheduler stopped"})
}

func (h *SyncHandler) GetSchedulerStatus(c *gin.Context) {
	scheduler := sync.GetScheduler()
	c.JSON(http.StatusOK, gin.H{
		"running": scheduler.IsRunning(),
		"workers": len(scheduler.GetAllStatuses()),
	})
}
