package handlers

import (
	"net/http"

	"one-mail/backend/internal/access"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	access *access.Access
}

type LoginRequest struct {
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

func NewAuthHandler(instanceAccess *access.Access) *AuthHandler {
	return &AuthHandler{access: instanceAccess}
}

func (h *AuthHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"password_required": h.access.PasswordRequired(),
		"authenticated":     h.access.Authorized(c.GetHeader("Authorization")),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		return
	}
	token, err := h.access.Login(req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		return
	}
	c.JSON(http.StatusOK, AuthResponse{Token: token})
}
