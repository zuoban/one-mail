package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"one-mail/backend/database"
	"one-mail/backend/internal/access"
	"one-mail/backend/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func routerRequest(r http.Handler, method, path, authorization, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRouterAuthenticationModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "router.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.EmailAccount{}); err != nil {
		t.Fatal(err)
	}
	oldDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = oldDB })

	for _, password := range []string{"", "instance-password"} {
		t.Run(password, func(t *testing.T) {
			instanceAccess, err := access.New(password)
			if err != nil {
				t.Fatal(err)
			}
			r := buildRouter(instanceAccess)
			status := routerRequest(r, http.MethodGet, "/api/auth/status", "Bearer old-user-token", "")
			var state struct {
				PasswordRequired bool `json:"password_required"`
				Authenticated    bool `json:"authenticated"`
			}
			if err := json.Unmarshal(status.Body.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if status.Code != http.StatusOK || state.PasswordRequired != (password != "") || state.Authenticated != (password == "") {
				t.Fatalf("unexpected public status: %d %s", status.Code, status.Body.String())
			}
			if password != "" {
				for _, route := range r.Routes() {
					if !strings.HasPrefix(route.Path, "/api/") || route.Path == "/api/auth/status" || route.Path == "/api/auth/login" {
						continue
					}
					path := strings.ReplaceAll(route.Path, ":id", "1")
					for _, header := range []string{"", "Bearer invalid-or-old-user-token"} {
						w := routerRequest(r, route.Method, path, header, `{}`)
						if w.Code != http.StatusUnauthorized {
							t.Fatalf("business route %s %s returned %d without authorization", route.Method, route.Path, w.Code)
						}
					}
				}
			}

			body, err := json.Marshal(map[string]string{"password": password})
			if err != nil {
				t.Fatal(err)
			}
			login := routerRequest(r, http.MethodPost, "/api/auth/login", "Bearer obsolete", string(body))
			var session struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
				t.Fatal(err)
			}
			if login.Code != http.StatusOK || (password != "" && session.Token == "") {
				t.Fatalf("public login returned %d: %s", login.Code, login.Body.String())
			}
			headers := []string{"Bearer " + session.Token}
			if password == "" {
				headers = []string{"", "Bearer broken", "Bearer old-user-token"}
			}
			for _, header := range headers {
				w := routerRequest(r, http.MethodGet, "/api/accounts", header, "")
				if w.Code != http.StatusOK {
					t.Fatalf("accessible business route returned %d: %s", w.Code, w.Body.String())
				}
			}
			for _, removed := range []struct {
				method string
				path   string
			}{
				{http.MethodPost, "/api/auth/register"},
				{http.MethodGet, "/api/auth/me"},
				{http.MethodPut, "/api/auth/profile"},
				{http.MethodPut, "/api/auth/password"},
			} {
				for _, header := range []string{"", "Bearer " + session.Token} {
					w := routerRequest(r, removed.method, removed.path, header, `{}`)
					if w.Code != http.StatusNotFound {
						t.Fatalf("removed route %s %s returned %d", removed.method, removed.path, w.Code)
					}
				}
			}
			if w := routerRequest(r, http.MethodGet, "/health", "", ""); w.Code != http.StatusOK {
				t.Fatalf("public health endpoint returned %d", w.Code)
			}
		})
	}
}
