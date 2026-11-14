package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"one-mail/backend/internal/access"

	"github.com/gin-gonic/gin"
)

func TestAuthenticationStatusAndLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, password := range []string{"", "instance-password", " "} {
		t.Run(password, func(t *testing.T) {
			instanceAccess, err := access.New(password)
			if err != nil {
				t.Fatal(err)
			}
			handler := NewAuthHandler(instanceAccess)
			r := gin.New()
			r.GET("/api/auth/status", handler.Status)
			r.POST("/api/auth/login", handler.Login)
			request := func(method, path, authorization, body string) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Authorization", authorization)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			assertStatus := func(authorization string, authenticated bool) {
				t.Helper()
				w := request(http.MethodGet, "/api/auth/status", authorization, "")
				var status struct {
					PasswordRequired bool `json:"password_required"`
					Authenticated    bool `json:"authenticated"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusOK || status.PasswordRequired != (password != "") || status.Authenticated != authenticated {
					t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
				}
			}
			assertStatus("", password == "")
			assertStatus("Bearer broken", password == "")
			if password != "" {
				for _, body := range []string{`{"password":"wrong"}`, `{}`, `{"password":null}`, `{"password":42}`, `invalid-json`} {
					w := request(http.MethodPost, "/api/auth/login", "", body)
					if w.Code != http.StatusUnauthorized || w.Body.String() != `{"error":"密码错误"}` {
						t.Fatalf("invalid login returned %d: %s", w.Code, w.Body.String())
					}
				}
			}
			body, err := json.Marshal(LoginRequest{Password: password})
			if err != nil {
				t.Fatal(err)
			}
			w := request(http.MethodPost, "/api/auth/login", "Bearer obsolete", string(body))
			var response map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || len(response) != 1 {
				t.Fatalf("login returned unexpected fields: %d %s", w.Code, w.Body.String())
			}
			token, exists := response["token"]
			if !exists || (password != "" && token == "") {
				t.Fatal("password login did not return a session token")
			}
			assertStatus("Bearer "+token, true)
		})
	}
}

func TestLoginFailureCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceAccess, err := access.New("instance-password")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthHandler(instanceAccess)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	handler.failures.now = func() time.Time { return now }
	r := gin.New()
	r.POST("/login", handler.Login)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("X-Forwarded-For", "203.0.113.42")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i := range loginFailureLimit {
		body := `{"password":"wrong"}`
		if i%2 == 0 {
			body = `invalid-json`
		}
		if w := request(body); w.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d returned %d", i+1, w.Code)
		}
		if i == 0 {
			if w := request(`{"password":"instance-password"}`); w.Code != http.StatusOK {
				t.Fatalf("valid login before cooldown returned %d", w.Code)
			}
		}
	}
	for _, body := range []string{`{"password":"wrong"}`, `{"password":"instance-password"}`, `invalid-json`} {
		w := request(body)
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
			t.Fatalf("cooldown returned %d, retry-after=%q", w.Code, w.Header().Get("Retry-After"))
		}
	}
	probe := &loginBodyProbe{}
	req := httptest.NewRequest(http.MethodPost, "/login", probe)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests || probe.read {
		t.Fatalf("cooldown must reject before reading credentials: status=%d read=%t", w.Code, probe.read)
	}
	now = now.Add(loginFailureWindow - 100*time.Millisecond)
	if w := request(`{}`); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("fractional cooldown returned %d, retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
	now = now.Add(100 * time.Millisecond)
	if w := request(`{"password":"instance-password"}`); w.Code != http.StatusOK || w.Header().Get("Retry-After") != "" {
		t.Fatalf("valid login after cooldown returned %d", w.Code)
	}
	if w := request(`{"password":"wrong"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("fresh failure window returned %d", w.Code)
	}
}

func TestConcurrentLoginFailuresUseOneInstanceBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceAccess, err := access.New("instance-password")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthHandler(instanceAccess)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	handler.failures.now = func() time.Time { return now }
	r := gin.New()
	r.POST("/login", handler.Login)
	var unauthorized, limited, unexpected atomic.Int64
	var group sync.WaitGroup
	for i := range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"password":"wrong"}`))
			req.RemoteAddr = "203.0.113." + strconv.Itoa(i+1) + ":1234"
			req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			switch w.Code {
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	group.Wait()
	if unauthorized.Load() != loginFailureLimit || limited.Load() != 64-loginFailureLimit || unexpected.Load() != 0 {
		t.Fatalf("unexpected concurrent outcomes: unauthorized=%d limited=%d unexpected=%d", unauthorized.Load(), limited.Load(), unexpected.Load())
	}
	if handler.failures.failures != loginFailureLimit {
		t.Fatalf("failure count grew beyond its fixed bound: %d", handler.failures.failures)
	}
}

func TestPasswordlessLoginBypassesFailureLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceAccess, err := access.New("")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthHandler(instanceAccess)
	handler.failures.now = func() time.Time { panic("passwordless mode used the limiter") }
	r := gin.New()
	r.POST("/login", handler.Login)
	for range loginFailureLimit * 2 {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("passwordless login returned %d", w.Code)
		}
	}
}

type loginBodyProbe struct {
	read bool
}

func (p *loginBodyProbe) Read(_ []byte) (int, error) {
	p.read = true
	return 0, io.EOF
}
