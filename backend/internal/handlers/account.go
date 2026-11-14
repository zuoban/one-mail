package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"one-mail/backend/internal/application"
)

type AccountHandler struct{ service *application.AccountService }

func NewAccountHandler(service *application.AccountService) *AccountHandler {
	return &AccountHandler{service: service}
}

func respondError(c *gin.Context, err error) {
	kind, message := application.ErrorDetails(err)
	status := http.StatusInternalServerError
	switch kind {
	case application.Invalid:
		status = http.StatusBadRequest
	case application.NotFound:
		status = http.StatusNotFound
	case application.Conflict:
		status = http.StatusConflict
	case application.Upstream:
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{"error": message})
}

func requestID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, strconv.IntSize)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid id is required"})
		return 0, false
	}
	return uint(id), true
}

func (h *AccountHandler) ListProviders(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": application.Providers()})
}

func (h *AccountHandler) ListAccounts(c *gin.Context) {
	accounts, err := h.service.List(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

func (h *AccountHandler) AddAccount(c *gin.Context) {
	var req application.CreateAccount
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account request"})
		return
	}
	account, refreshErr, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}
	response := gin.H{"data": account}
	if refreshErr != nil {
		_, message := application.ErrorDetails(refreshErr)
		response["refresh_error"] = message
	}
	c.JSON(http.StatusOK, response)
}

func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

func (h *AccountHandler) UpdateAccount(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	var req application.UpdateAccount
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account request"})
		return
	}
	account, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": account})
}

func (h *AccountHandler) TestAccount(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	if err := h.service.Test(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "connection successful"})
}
