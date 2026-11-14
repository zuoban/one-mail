package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthMiddlewareEnforcesActiveSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB, oldConfig := database.DB, config.AppConfig
	t.Cleanup(func() { database.DB, config.AppConfig = oldDB, oldConfig })
	t.Setenv("JWT_SECRET", strings.Repeat("middleware-key-", 3))
	config.AppConfig = &config.Config{JWT: config.JWTConfig{Expires: "1h"}}
	if err := utils.InitJWT(); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	database.DB = db
	user := models.User{Username: "alice", Password: "hash", SessionVersion: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, err := utils.GenerateToken(user.ID, user.Username, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(AuthMiddleware())
	for _, path := range []string{"/private", "/api/auth/login/admin", "/api/auth/register/extra", "/health/admin"} {
		r.GET(path, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}
	request := func(path, authorization string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for _, path := range []string{"/private?token=" + token, "/api/auth/login/admin", "/api/auth/register/extra", "/health/admin"} {
		if got := request(path, ""); got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated path %q returned %d", path, got)
		}
	}
	if got := request("/private", token); got != http.StatusUnauthorized {
		t.Fatalf("raw authorization token returned %d", got)
	}
	if got := request("/private", "Bearer "+token); got != http.StatusNoContent {
		t.Fatalf("valid session returned %d", got)
	}
	if err := db.Model(&user).Update("session_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if got := request("/private", "Bearer "+token); got != http.StatusUnauthorized {
		t.Fatalf("revoked session returned %d", got)
	}
	token, err = utils.GenerateToken(user.ID, user.Username, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := request("/private", "Bearer "+token); got != http.StatusNoContent {
		t.Fatalf("new session returned %d", got)
	}
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	if got := request("/private", "Bearer "+token); got != http.StatusUnauthorized {
		t.Fatalf("deleted user returned %d", got)
	}
	database.DB = nil
	if got := request("/private", "Bearer "+token); got != http.StatusServiceUnavailable {
		t.Fatalf("unavailable authentication store returned %d", got)
	}
}
