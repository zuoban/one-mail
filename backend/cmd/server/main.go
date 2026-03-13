package main

import (
	"fmt"
	"log"
	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"
	"one-mail/backend/internal/services/sync"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := database.InitDatabase(); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	if err := sync.InitSyncCache(); err != nil {
		log.Printf("Warning: Failed to init sync cache: %v", err)
	}

	scheduler := sync.GetScheduler()
	scheduler.Start()
	log.Println("Sync scheduler started")

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())

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

		api.GET("/emails", emailHandler.ListEmails)
		api.GET("/emails/:id", emailHandler.GetEmail)
		api.POST("/emails/:id/read", emailHandler.MarkAsRead)
		api.POST("/emails/:id/unread", emailHandler.MarkAsUnread)
		api.DELETE("/emails/:id", emailHandler.DeleteEmail)
		api.POST("/accounts/:id/sync", syncHandler.TriggerSync)

		api.GET("/sync/status", syncHandler.GetAllStatuses)
		api.GET("/sync/status/:id", syncHandler.GetStatus)
		api.PUT("/accounts/:id/sync/config", syncHandler.UpdateConfig)
		api.POST("/sync/start", syncHandler.StartScheduler)
		api.POST("/sync/stop", syncHandler.StopScheduler)
		api.GET("/sync", syncHandler.GetSchedulerStatus)
		api.GET("/sync/logs", syncHandler.GetSyncLogs)
		api.GET("/sync/logs/:account_id", syncHandler.GetSyncLogs)
		api.DELETE("/sync/logs/:account_id", syncHandler.ClearSyncLogs)
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
