package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"one-mail/backend/internal/access"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	access   *access.Access
	failures *loginFailureLimiter
}

type LoginRequest struct {
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

func NewAuthHandler(instanceAccess *access.Access) *AuthHandler {
	return &AuthHandler{access: instanceAccess, failures: newLoginFailureLimiter()}
}

func (h *AuthHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"password_required": h.access.PasswordRequired(),
		"authenticated":     h.access.Authorized(c.GetHeader("Authorization")),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	if h.access.PasswordRequired() {
		if retryAfter := h.failures.retryAfter(); retryAfter > 0 {
			h.rejectLimitedLogin(c, retryAfter)
			return
		}
	}
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var bodyTooLarge *http.MaxBytesError
		if errors.As(err, &bodyTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		h.rejectLogin(c)
		return
	}
	var token string
	var err error
	if h.access.PasswordRequired() {
		var retryAfter int
		token, err, retryAfter = h.failures.login(h.access, req.Password)
		if retryAfter > 0 {
			h.rejectLimitedLogin(c, retryAfter)
			return
		}
	} else {
		token, err = h.access.Login(req.Password)
	}
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		return
	}
	c.JSON(http.StatusOK, AuthResponse{Token: token})
}

func (h *AuthHandler) rejectLogin(c *gin.Context) {
	if h.access.PasswordRequired() {
		if retryAfter := h.failures.recordFailure(); retryAfter > 0 {
			h.rejectLimitedLogin(c, retryAfter)
			return
		}
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
}

func (h *AuthHandler) rejectLimitedLogin(c *gin.Context, retryAfter int) {
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "登录失败次数过多，请稍后重试"})
}
