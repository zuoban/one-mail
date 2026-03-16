package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"
	syncservice "one-mail/backend/internal/services/sync"

	"github.com/gin-gonic/gin"
)

type cachedImage struct {
	data        []byte
	contentType string
	expiry      time.Time
}

var imageCache struct {
	sync.RWMutex
	data map[string]cachedImage
}

var imageCacheInit sync.Mutex

func init() {
	imageCache.data = make(map[string]cachedImage)
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			imageCache.Lock()
			now := time.Now()
			for k, v := range imageCache.data {
				if now.After(v.expiry) {
					delete(imageCache.data, k)
				}
			}
			imageCache.Unlock()
		}
	}()
}

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := database.InitDatabase(); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	if err := syncservice.InitSyncCache(); err != nil {
		log.Printf("Warning: Failed to init sync cache: %v", err)
	}

	scheduler := syncservice.GetScheduler()
	scheduler.Start()
	log.Println("Sync scheduler started")

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())

	r.GET("/api/proxy/image", func(c *gin.Context) {
		imgURL := c.Query("url")
		if imgURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
			return
		}

		parsedURL, err := url.Parse(imgURL)
		if err != nil || parsedURL.Scheme == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
			return
		}

		imageCache.RLock()
		if cached, ok := imageCache.data[imgURL]; ok {
			imageCache.RUnlock()
			c.Header("Content-Type", cached.contentType)
			c.Header("Cache-Control", "public, max-age=86400")
			c.Writer.Write(cached.data)
			return
		}
		imageCache.RUnlock()

		req, err := http.NewRequest("GET", imgURL, nil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to create request"})
			return
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch image"})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read image"})
			return
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "image/jpeg"
		}

		imageCache.Lock()
		imageCache.data[imgURL] = cachedImage{
			data:        body,
			contentType: contentType,
			expiry:      time.Now().Add(10 * time.Minute),
		}
		imageCache.Unlock()

		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "public, max-age=86400")
		c.Writer.Write(body)
	})

	accountHandler := handlers.NewAccountHandler()
	emailHandler := handlers.NewEmailHandler()
	authHandler := handlers.NewAuthHandler()
	syncHandler := handlers.NewSyncHandler()

	auth := r.Group("/api/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware())
	{
		api.GET("/auth/me", authHandler.Me)
		api.PUT("/auth/profile", authHandler.UpdateProfile)
		api.PUT("/auth/password", authHandler.ChangePassword)

		api.GET("/accounts", accountHandler.ListAccounts)
		api.POST("/accounts", accountHandler.AddAccount)
		api.PUT("/accounts/:id", accountHandler.UpdateAccount)
		api.DELETE("/accounts/:id", accountHandler.DeleteAccount)
		api.POST("/accounts/:id/test", accountHandler.TestAccount)
		api.GET("/accounts/:id/folders", accountHandler.ListFolders)

		api.GET("/emails", emailHandler.ListEmails)
		api.GET("/emails/:id", emailHandler.GetEmail)
		api.POST("/emails/:id/read", emailHandler.MarkAsRead)
		api.POST("/emails/:id/unread", emailHandler.MarkAsUnread)
		api.DELETE("/emails/:id", emailHandler.DeleteEmail)
		api.POST("/accounts/:id/sync", syncHandler.TriggerSync)
		api.GET("/accounts/:id/sync/preview", syncHandler.PreviewSync)

		api.GET("/sync/status", syncHandler.GetAllStatuses)
		api.GET("/sync/status/:id", syncHandler.GetStatus)
		api.PUT("/accounts/:id/sync/config", syncHandler.UpdateConfig)
		api.POST("/sync/start", syncHandler.StartScheduler)
		api.POST("/sync/stop", syncHandler.StopScheduler)
		api.GET("/sync/logs", syncHandler.GetSyncLogs)
		api.GET("/sync/logs/:account_id", syncHandler.GetSyncLogs)
		api.DELETE("/sync/logs/:account_id", syncHandler.ClearSyncLogs)

		api.GET("/telegram/config", handlers.GetTelegramConfig)
		api.PUT("/telegram/config", handlers.UpdateTelegramConfig)
		api.POST("/telegram/test", handlers.TestTelegramConnection)
		api.POST("/telegram/send/:id", handlers.SendEmailToTelegram)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
