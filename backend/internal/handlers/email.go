package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"one-mail/backend/internal/application"
)

type EmailHandler struct{ service *application.MailService }

func NewEmailHandler(service *application.MailService) *EmailHandler {
	return &EmailHandler{service: service}
}

func (h *EmailHandler) ListEmails(c *gin.Context) {
	var query application.ListEmails
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email query"})
		return
	}
	emails, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": emails, "total": len(emails)})
}

func (h *EmailHandler) GetEmail(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	email, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": email})
}

func (h *EmailHandler) RefreshEmails(c *gin.Context) {
	var query struct {
		AccountID uint `form:"account_id"`
		Limit     int  `form:"limit"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid refresh query"})
		return
	}
	summary, err := h.service.Refresh(c.Request.Context(), query.AccountID, query.Limit)
	if err != nil {
		kind, message := application.ErrorDetails(err)
		if kind == application.Upstream {
			c.JSON(http.StatusBadGateway, gin.H{"error": message, "refreshed_accounts": summary.Refreshed, "new_count": summary.NewCount, "errors": summary.Errors})
			return
		}
		respondError(c, err)
		return
	}
	message := "refreshed"
	if summary.Refreshed == 0 && len(summary.Errors) == 0 {
		message = "no active accounts"
	}
	c.JSON(http.StatusOK, gin.H{"message": message, "refreshed_accounts": summary.Refreshed, "new_count": summary.NewCount, "errors": summary.Errors})
}

func (h *EmailHandler) RefreshAccountEmails(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	var query struct {
		Limit int `form:"limit"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid refresh query"})
		return
	}
	count, err := h.service.RefreshAccount(c.Request.Context(), id, query.Limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "refreshed", "new_count": count})
}
