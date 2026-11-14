package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
