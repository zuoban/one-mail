package middleware

import (
	"log"
	"net/http"
	"strings"

	"one-mail/backend/config"
	"one-mail/backend/internal/utils"

	"github.com/gin-gonic/gin"
)

var publicPaths = []string{
	"/api/auth/register",
	"/api/auth/login",
	"/health",
}

func isPublicPath(path string) bool {
	for _, p := range publicPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		token := c.GetHeader("Authorization")
		if token == "" {
			token = c.Query("token")
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}

		claims, err := utils.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)

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