package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGzipMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GzipMiddleware())

	r.GET("/small", func(c *gin.Context) {
		c.String(http.StatusOK, "hello")
	})
	largePayload := strings.Repeat("email-body-content-", 120)
	r.GET("/large", func(c *gin.Context) {
		c.String(http.StatusOK, largePayload)
	})

	// Small response should not be compressed
	req := httptest.NewRequest(http.MethodGet, "/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for small body, got %q", w.Header().Get("Content-Encoding"))
	}
	if w.Body.String() != "hello" {
		t.Fatalf("unexpected small body: %q", w.Body.String())
	}

	// Large response should be gzipped when Accept-Encoding includes gzip
	req = httptest.NewRequest(http.MethodGet, "/large", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip Content-Encoding, got %q", w.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("failed to open gzip reader: %v", err)
	}
	defer gr.Close()
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to read gzip body: %v", err)
	}
	if string(decompressed) != largePayload {
		t.Fatalf("decompressed payload mismatch")
	}
}
