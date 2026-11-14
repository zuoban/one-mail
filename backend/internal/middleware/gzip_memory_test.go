package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLargeGzipResponseAndEncodingNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := strings.Repeat("large unicode 邮件正文 ", 300000)
	router := gin.New()
	router.Use(GzipMiddleware())
	router.GET("/", func(ctx *gin.Context) { ctx.String(http.StatusOK, payload) })
	for _, encoding := range []string{"gzip", "GZIP; q=0.5", "*;q=1", "gzip;q=0", "gzip;q=0, *;q=1", "notgzip", "gzip;q=invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", encoding)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		body := recorder.Body.Bytes()
		compressed := encoding == "gzip" || encoding == "GZIP; q=0.5" || encoding == "*;q=1"
		if (recorder.Header().Get("Content-Encoding") == "gzip") != compressed {
			t.Fatalf("wrong encoding for %q: %v", encoding, recorder.Header())
		}
		if !strings.Contains(recorder.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("missing cache variation for %q", encoding)
		}
		if compressed {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			body, err = io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if string(body) != payload {
			t.Fatalf("large response changed for %q", encoding)
		}
	}
}

func TestRecycledGzipBufferDropsLargeBackingStorage(t *testing.T) {
	for _, size := range []int{1024, maxReusableGzipBuffer, maxReusableGzipBuffer * 32} {
		buffer := bytes.NewBuffer(make([]byte, size))
		recycleGzipBuffer(buffer)
		if buffer.Len() != 0 || buffer.Cap() > maxReusableGzipBuffer {
			t.Fatalf("recycled buffer retained a large response: len=%d cap=%d", buffer.Len(), buffer.Cap())
		}
	}
}

func BenchmarkGzipLargeBody(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := strings.Repeat("large-mail-body-", (8<<20)/16)
	router := gin.New()
	router.Use(GzipMiddleware())
	router.GET("/", func(ctx *gin.Context) { ctx.String(http.StatusOK, payload) })
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", "gzip")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Encoding") != "gzip" {
			b.Fatal("large response was not compressed")
		}
	}
}
