package middleware

import (
	"bytes"
	"compress/gzip"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"one-mail/backend/config"
	"one-mail/backend/internal/access"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware(instanceAccess *access.Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !instanceAccess.Authorized(c.GetHeader("Authorization")) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			return
		}
		c.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	// 从配置获取允许的域名列表
	allowedOrigins := getAllowedOrigins()

	if len(allowedOrigins) > 0 {
		log.Printf("CORS: 允许的域名: %v", allowedOrigins)
	} else {
		log.Println("⚠️  警告: CORS 允许所有来源，生产环境请配置 cors.allowed_origins")
	}

	allowedOriginsSet := make(map[string]bool)
	for _, origin := range allowedOrigins {
		allowedOriginsSet[origin] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// 如果配置了允许的域名列表，则检查请求来源
		if len(allowedOriginsSet) > 0 {
			if allowedOriginsSet[origin] {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			}
			// 不在允许列表中的来源不会设置 Access-Control-Allow-Origin
		} else {
			// 未配置允许列表时，允许所有来源（仅开发环境）
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}

		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// getAllowedOrigins 获取允许的域名列表
func getAllowedOrigins() []string {
	// 1. 优先从环境变量获取（逗号分隔）
	// envOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	// if envOrigins != "" {
	// 	return strings.Split(envOrigins, ",")
	// }

	// 2. 从配置文件获取
	if config.AppConfig != nil && len(config.AppConfig.CORS.AllowedOrigins) > 0 {
		return config.AppConfig.CORS.AllowedOrigins
	}

	// 3. 返回空列表，表示允许所有
	return nil
}

var gzipBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

const maxReusableGzipBuffer = 256 << 10

func recycleGzipBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > maxReusableGzipBuffer {
		*buffer = bytes.Buffer{}
	} else {
		buffer.Reset()
	}
	gzipBufferPool.Put(buffer)
}

func acceptsGzip(header string) bool {
	wildcard := false
	for _, encoding := range strings.Split(header, ",") {
		parts := strings.Split(encoding, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name != "gzip" && name != "*" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if name == "gzip" {
			return quality > 0
		}
		wildcard = quality > 0
	}
	return wildcard
}

var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	buf        *bytes.Buffer
	statusCode int
	wroteCode  bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.wroteCode = true
}

func (w *gzipResponseWriter) WriteHeaderNow() {}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	return w.buf.Write(data)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.buf.WriteString(s)
}

func (w *gzipResponseWriter) Status() int {
	if w.wroteCode {
		return w.statusCode
	}
	return w.ResponseWriter.Status()
}

func (w *gzipResponseWriter) Size() int {
	return w.buf.Len()
}

func GzipMiddleware() gin.HandlerFunc {
	const minCompressBytes = 1024
	return func(c *gin.Context) {
		if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			c.Next()
			return
		}
		c.Header("Vary", appendVary(strings.Join(c.Writer.Header().Values("Vary"), ", "), "Accept-Encoding"))
		if !acceptsGzip(c.GetHeader("Accept-Encoding")) {
			c.Next()
			return
		}

		buf := gzipBufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer recycleGzipBuffer(buf)

		origWriter := c.Writer
		gw := &gzipResponseWriter{
			ResponseWriter: origWriter,
			buf:            buf,
			statusCode:     http.StatusOK,
		}
		c.Writer = gw
		defer func() {
			c.Writer = origWriter
		}()

		c.Next()

		status := gw.statusCode
		if !gw.wroteCode {
			status = http.StatusOK
		}
		headers := origWriter.Header()
		if status == http.StatusNoContent ||
			status == http.StatusNotModified ||
			headers.Get("Content-Encoding") != "" ||
			buf.Len() < minCompressBytes {
			if gw.wroteCode {
				origWriter.WriteHeader(status)
			}
			if buf.Len() > 0 {
				_, _ = origWriter.Write(buf.Bytes())
			}
			return
		}

		headers.Set("Content-Encoding", "gzip")
		headers.Del("Content-Length")
		if gw.wroteCode {
			origWriter.WriteHeader(status)
		}

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(origWriter)
		_, _ = gz.Write(buf.Bytes())
		_ = gz.Close()
		gz.Reset(nil)
		gzipWriterPool.Put(gz)
	}
}

func appendVary(current, value string) string {
	for _, token := range strings.Split(current, ",") {
		if strings.EqualFold(strings.TrimSpace(token), value) || strings.TrimSpace(token) == "*" {
			return current
		}
	}
	if current == "" {
		return value
	}
	return current + ", " + value
}
