package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/sync"

	"github.com/gin-gonic/gin"
)

type SyncHandler struct{}

type SyncConfigRequest struct {
	SyncFolders    string `json:"sync_folders"`
	EnableAutoSync bool   `json:"enable_auto_sync"`
}

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

	id, err := parseUint(accountID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}

	status := sync.GetScheduler().GetStatus(id)

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
		"sync_folders":     req.SyncFolders,
		"enable_auto_sync": req.EnableAutoSync,
	}

	if err := database.GetDB().Model(&account).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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

func (h *SyncHandler) PreviewSync(c *gin.Context) {
	accountID := c.Param("id")
	userID := c.GetUint("user_id")
	folder := c.DefaultQuery("folder", "INBOX")

	var account models.EmailAccount
	if err := database.GetDB().
		Where("id = ? AND user_id = ?", accountID, userID).
		First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	var syncState models.SyncState
	database.GetDB().Where("account_id = ? AND folder = ?", account.ID, folder).First(&syncState)

	client := imap.NewClient(&account)
	if err := client.Connect(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer client.Disconnect()

	status, statusErr := clientStatus(client, folder)
	emails, maxUID, err := client.FetchEmailsIncremental(folder, syncState.LastUID, 20)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var firstUID uint
	var lastFetchedUID uint
	if len(emails) > 0 {
		firstUID = emails[0].UID
		lastFetchedUID = emails[len(emails)-1].UID
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"folder":        folder,
			"last_uid":      syncState.LastUID,
			"status":        status,
			"status_error":  statusErr,
			"fetched_count": len(emails),
			"max_uid":       maxUID,
			"first_uid":     firstUID,
			"last_fetched":  lastFetchedUID,
		},
	})
}

func clientStatus(client *imap.Client, folder string) (map[string]interface{}, string) {
	status, err := client.Status(folder)
	if err != nil {
		return nil, err.Error()
	}

	result := map[string]interface{}{
		"mailbox": folder,
	}
	if status.NumMessages != nil {
		result["messages"] = *status.NumMessages
	}
	if status.NumUnseen != nil {
		result["unseen"] = *status.NumUnseen
	}
	if status.UIDNext > 0 {
		result["uid_next"] = status.UIDNext
	}
	return result, ""
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

func (h *SyncHandler) GetSyncLogs(c *gin.Context) {
	userID := c.GetUint("user_id")
	accountIDStr := c.Param("account_id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize > 100 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}

	var accountIDs []uint
	if accountIDStr != "" {
		accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
			return
		}
		var account models.EmailAccount
		if err := database.GetDB().Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		accountIDs = []uint{uint(accountID)}
	} else {
		var accounts []models.EmailAccount
		if err := database.GetDB().Where("user_id = ?", userID).Find(&accounts).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, acc := range accounts {
			accountIDs = append(accountIDs, acc.ID)
		}
	}

	if len(accountIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []models.SyncLog{}, "total": 0, "page": page, "page_size": pageSize})
		return
	}

	var total int64
	db := database.GetDB().Model(&models.SyncLog{}).Where("account_id IN ?", accountIDs)
	db.Count(&total)

	var logs []models.SyncLog
	offset := (page - 1) * pageSize
	if err := db.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

func (h *SyncHandler) ClearSyncLogs(c *gin.Context) {
	userID := c.GetUint("user_id")
	accountIDStr := c.Param("account_id")

	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account_id"})
		return
	}

	var account models.EmailAccount
	if err := database.GetDB().Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	if err := database.GetDB().Where("account_id = ?", accountID).Delete(&models.SyncLog{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "sync logs cleared"})
}

func parseUint(s string) (uint, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(val), nil
}
