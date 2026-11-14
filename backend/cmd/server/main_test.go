package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"one-mail/backend/internal/access"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/telegram"

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

func testRouter(t *testing.T, instanceAccess *access.Access, db *gorm.DB) *gin.Engine {
	t.Helper()
	pool := imap.NewConnectionPool()
	t.Cleanup(func() { _ = pool.Close() })
	return buildRouter(instanceAccess, db, pool, telegram.NewClient())
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

	for _, password := range []string{"", "instance-password"} {
		t.Run(password, func(t *testing.T) {
			instanceAccess, err := access.New(password)
			if err != nil {
				t.Fatal(err)
			}
			r := testRouter(t, instanceAccess, db)
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
				{http.MethodPost, "/api/emails/1/read"},
				{http.MethodPost, "/api/emails/1/unread"},
				{http.MethodPost, "/api/emails/batch/read"},
				{http.MethodDelete, "/api/emails/1"},
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

func TestHTTPServerBudgets(t *testing.T) {
	server := newHTTPServer(":8080", http.NotFoundHandler())
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected read/idle timeouts: %+v", server)
	}
	if server.WriteTimeout <= 35*time.Second || shutdownTimeout <= 35*time.Second {
		t.Fatal("HTTP and shutdown budgets must allow a complete 35-second refresh")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceAccess, err := access.New("instance-password")
	if err != nil {
		t.Fatal(err)
	}
	r := testRouter(t, instanceAccess, nil)
	body := `{"password":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`
	for _, contentLength := range []int64{int64(len(body)), -1} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.ContentLength = contentLength
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("content length %d returned %d", contentLength, w.Code)
		}
	}
}

func TestReadinessRejectsMissingAndClosedDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceAccess, err := access.New("")
	if err != nil {
		t.Fatal(err)
	}
	r := testRouter(t, instanceAccess, nil)
	if w := routerRequest(r, http.MethodGet, "/health", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing database returned %d", w.Code)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "health.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	r = testRouter(t, instanceAccess, db)
	if w := routerRequest(r, http.MethodGet, "/health", "", ""); w.Code != http.StatusOK {
		t.Fatalf("healthy database returned %d", w.Code)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if w := routerRequest(r, http.MethodGet, "/health", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed database returned %d", w.Code)
	}
}

func TestReadinessUsesRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "health-cancel.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	health(db)(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancelled readiness returned %d", w.Code)
	}
}

func TestServeShutsDownOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(ctx, newHTTPServer("127.0.0.1:0", http.NotFoundHandler())); err != nil {
		t.Fatalf("cancelled server failed to shut down: %v", err)
	}
}

func TestRequestLogOmitsSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(oldOutput) })
	r := gin.New()
	r.Use(requestLogger())
	r.POST("/api/accounts/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/private-id?subject=private-subject&token=private-token", strings.NewReader(`{"password":"private-password"}`))
	req.Header.Set("Authorization", "Bearer private-authorization")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	line := output.String()
	for _, secret := range []string{"private-id", "private-subject", "private-token", "private-password", "private-authorization"} {
		if strings.Contains(line, secret) {
			t.Fatalf("request log disclosed %q", secret)
		}
	}
	if !strings.Contains(line, `route="/api/accounts/:id"`) || !strings.Contains(line, "status=200") || !strings.Contains(line, "duration_ms=") {
		t.Fatalf("request log lacks operational fields: %s", line)
	}
}
