package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"one-mail/backend/internal/access"

	"github.com/gin-gonic/gin"
)

func TestAuthMiddlewareOptionalPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, password := range []string{"", "instance-password"} {
		t.Run(password, func(t *testing.T) {
			instanceAccess, err := access.New(password)
			if err != nil {
				t.Fatal(err)
			}
			token, err := instanceAccess.Login(password)
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(AuthMiddleware(instanceAccess))
			for _, path := range []string{"/private", "/api/auth/login/admin", "/api/auth/register/extra", "/health/admin"} {
				r.GET(path, func(c *gin.Context) {
					if _, exists := c.Get("user_id"); exists {
						t.Error("authentication injected a user identity")
					}
					if _, exists := c.Get("username"); exists {
						t.Error("authentication injected a username")
					}
					c.Status(http.StatusNoContent)
				})
			}
			for _, path := range []string{"/private?token=" + token, "/api/auth/login/admin", "/api/auth/register/extra", "/health/admin"} {
				for _, header := range []string{"", "Bearer broken", "Basic old-password"} {
					req := httptest.NewRequest(http.MethodGet, path, nil)
					req.Header.Set("Authorization", header)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					want := http.StatusNoContent
					if password != "" {
						want = http.StatusUnauthorized
					}
					if w.Code != want {
						t.Fatalf("%q with header %q returned %d, want %d", path, header, w.Code, want)
					}
				}
			}
			if password != "" {
				req := httptest.NewRequest(http.MethodGet, "/private", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusNoContent {
					t.Fatalf("authenticated request returned %d", w.Code)
				}
			}
		})
	}
}
