package main

import (
	"fmt"
	"log"
	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"

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

	r := gin.Default()
	r.Use(middleware.CORSMiddleware())

	accountHandler := handlers.NewAccountHandler()
	emailHandler := handlers.NewEmailHandler()
	authHandler := handlers.NewAuthHandler()

	auth := r.Group("/api/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware())
	{
		api.GET("/auth/me", authHandler.Me)

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
		api.POST("/accounts/:id/sync", emailHandler.SyncEmails)
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
