package main

import (
	"fmt"
	"log"
	"os"

	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/access"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	instanceAccess, err := access.New(os.Getenv("ADMIN_PASSWORD"))
	if err != nil {
		log.Fatalf("Failed to initialize access: %v", err)
	}
	if err := database.InitDatabase(); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	gin.SetMode(cfg.Server.Mode)
	r := buildRouter(instanceAccess)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func buildRouter(instanceAccess *access.Access) *gin.Engine {
	r := gin.Default()
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.GzipMiddleware())

	accountHandler := handlers.NewAccountHandler()
	emailHandler := handlers.NewEmailHandler()
	authHandler := handlers.NewAuthHandler(instanceAccess)
	auth := r.Group("/api/auth")
	{
		auth.GET("/status", authHandler.Status)
		auth.POST("/login", authHandler.Login)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(instanceAccess))
	{

		api.GET("/accounts", accountHandler.ListAccounts)
		api.POST("/accounts", accountHandler.AddAccount)
		api.PUT("/accounts/:id", accountHandler.UpdateAccount)
		api.DELETE("/accounts/:id", accountHandler.DeleteAccount)
		api.POST("/accounts/:id/test", accountHandler.TestAccount)
		api.POST("/accounts/:id/refresh", emailHandler.RefreshAccountEmails)

		api.GET("/emails", emailHandler.ListEmails)
		api.POST("/emails/refresh", emailHandler.RefreshEmails)
		api.GET("/emails/:id", emailHandler.GetEmail)
		api.POST("/emails/:id/read", emailHandler.MarkAsRead)
		api.POST("/emails/:id/unread", emailHandler.MarkAsUnread)
		api.POST("/emails/batch/read", emailHandler.BatchMarkAsRead)
		api.DELETE("/emails/:id", emailHandler.DeleteEmail)
		api.GET("/telegram/config", handlers.GetTelegramConfig)
		api.PUT("/telegram/config", handlers.UpdateTelegramConfig)
		api.POST("/telegram/test", handlers.TestTelegramConnection)
		api.POST("/telegram/send/:id", handlers.SendEmailToTelegram)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return r
}
