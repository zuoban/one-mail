package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"one-mail/backend/internal/application"
)

type TelegramHandler struct{ service *application.TelegramService }

func NewTelegramHandler(service *application.TelegramService) *TelegramHandler {
	return &TelegramHandler{service: service}
}

func (h *TelegramHandler) GetTelegramConfig(c *gin.Context) {
	cfg, err := h.service.Config(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}

func (h *TelegramHandler) UpdateTelegramConfig(c *gin.Context) {
	var req application.UpdateTelegramConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram configuration request"})
		return
	}
	cfg, err := h.service.Update(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}

func (h *TelegramHandler) TestTelegramConnection(c *gin.Context) {
	var req application.UpdateTelegramConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram configuration request"})
		return
	}
	if err := h.service.Test(c.Request.Context(), req); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "测试消息发送成功"})
}

func (h *TelegramHandler) SendEmailToTelegram(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	if err := h.service.Forward(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "邮件已发送到 Telegram"})
}
