package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"one-mail/backend/config"
	"one-mail/backend/internal/middleware"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupSecurityHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = oldConfig })
	t.Setenv("JWT_SECRET", strings.Repeat("handler-test-key-", 3))
	config.AppConfig = &config.Config{JWT: config.JWTConfig{Expires: "1h"}}
	if err := utils.InitJWT(); err != nil {
		t.Fatal(err)
	}
	return setupEmailHandlerTestDB(t)
}

func securityRequest(t *testing.T, r http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPasswordChangeRevokesExistingSessions(t *testing.T) {
	db := setupSecurityHandlerDB(t)
	h := NewAuthHandler()
	r := gin.New()
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	private := r.Group("/private", middleware.AuthMiddleware())
	private.PUT("/password", h.ChangePassword)
	private.GET("/me", h.Me)

	w := securityRequest(t, r, http.MethodPost, "/register", "", `{"username":"alice","password":"old-password","email":"alice@example.com"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("registration returned %d: %s", w.Code, w.Body.String())
	}
	var registered AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	claims, err := utils.ValidateToken(registered.Token)
	if err != nil || claims.SessionVersion != 1 {
		t.Fatalf("registration did not initialize session version: %v", err)
	}
	w = securityRequest(t, r, http.MethodPut, "/private/password", registered.Token, `{"old_password":"wrong-password","new_password":"new-password"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong old password returned %d", w.Code)
	}
	var before models.User
	if err := db.First(&before, claims.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if before.SessionVersion != 1 || !utils.CheckPassword("old-password", before.Password) {
		t.Fatal("failed password change modified the stored session")
	}
	w = securityRequest(t, r, http.MethodPut, "/private/password", registered.Token, `{"old_password":"old-password","new_password":"new-password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("password change returned %d: %s", w.Code, w.Body.String())
	}
	var after models.User
	if err := db.First(&after, claims.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if after.SessionVersion != 2 || !utils.CheckPassword("new-password", after.Password) {
		t.Fatal("password and session version were not updated together")
	}
	w = securityRequest(t, r, http.MethodGet, "/private/me", registered.Token, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old token remained active: %d", w.Code)
	}
	w = securityRequest(t, r, http.MethodPost, "/login", "", `{"username":"alice","password":"old-password"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old password remained active: %d", w.Code)
	}
	w = securityRequest(t, r, http.MethodPost, "/login", "", `{"username":"alice","password":"new-password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("new password returned %d: %s", w.Code, w.Body.String())
	}
	var loggedIn AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &loggedIn); err != nil {
		t.Fatal(err)
	}
	claims, err = utils.ValidateToken(loggedIn.Token)
	if err != nil || claims.SessionVersion != 2 {
		t.Fatalf("login did not use current session version: %v", err)
	}
	w = securityRequest(t, r, http.MethodGet, "/private/me", loggedIn.Token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("new token failed: %d", w.Code)
	}
}
