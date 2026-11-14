package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/access"
	"one-mail/backend/internal/application"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"
	"one-mail/backend/internal/services/telegram"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxRequestBodyBytes = 1 << 20
	readinessTimeout    = 500 * time.Millisecond
	shutdownTimeout     = 45 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Printf("event=server_exit error=%q", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Set this before database initialization so its logging uses the same mode.
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		cfg.Server.Mode = mode
	}
	gin.SetMode(cfg.Server.Mode)
	instanceAccess, err := access.New(os.Getenv("ADMIN_PASSWORD"))
	if err != nil {
		return fmt.Errorf("initialize access: %w", err)
	}
	db, err := database.InitDatabase()
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get database pool: %w", err)
	}
	defer sqlDB.Close()
	pool := imap.NewConnectionPool()
	defer pool.Close()
	telegramClient := telegram.NewClient()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		logResourceStats(workersCtx, sqlDB, db, pool)
	}()
	go func() {
		defer workers.Done()
		telegram.NewDispatcher(db, telegramClient).Run(workersCtx)
	}()
	defer func() {
		cancelWorkers()
		workers.Wait()
	}()
	server := newHTTPServer(fmt.Sprintf(":%d", cfg.Server.Port), buildRouter(instanceAccess, db, pool, telegramClient))
	return serve(ctx, server)
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func serve(ctx context.Context, server *http.Server) error {
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("event=server_start address=%q", server.Addr)
		serveErr <- server.ListenAndServe()
	}()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}
	log.Print("event=server_shutdown")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

func logResourceStats(ctx context.Context, db *sql.DB, appDB *gorm.DB, pool *imap.ConnectionPool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := db.Stats()
			imapStats := pool.GetStats()
			refreshActive, refreshShared := mail.RefreshStats()
			var pending int64
			var failed int64
			countCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
			err := telegram.PendingTasks(appDB.WithContext(countCtx)).Count(&pending).Error
			if err == nil {
				err = telegram.FailedTasks(appDB.WithContext(countCtx)).Count(&failed).Error
			}
			cancel()
			log.Printf("event=resource_stats db_open=%d db_in_use=%d db_idle=%d db_wait_count=%d db_wait_duration_ms=%d imap_connections=%v imap_wait_count=%v imap_wait_duration_ms=%v refresh_active=%d refresh_shared=%d notification_pending=%d notification_failed=%d notification_stats_ok=%t",
				stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount, stats.WaitDuration.Milliseconds(), imapStats["total_connections"], imapStats["wait_count"], imapStats["wait_duration_ms"], refreshActive, refreshShared, pending, failed, err == nil)
		}
	}
}

func requestLimits() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxRequestBodyBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		// FullPath is the route template; URLs, headers and bodies may contain secrets.
		log.Printf("event=http_request method=%q route=%q status=%d duration_ms=%d",
			c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start).Milliseconds())
	}
}

func health(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		sqlDB, err := db.DB()
		if err == nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
			defer cancel()
			err = sqlDB.PingContext(ctx)
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func buildRouter(instanceAccess *access.Access, db *gorm.DB, pool *imap.ConnectionPool, telegramClient *telegram.Client) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger())
	r.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ interface{}) {
		log.Print("event=http_panic")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}))
	r.Use(requestLimits())
	r.Use(middleware.CORSMiddleware())
	r.Use(middleware.GzipMiddleware())

	mailService := application.NewMailService(db, pool, database.IsFTS5Enabled())
	accountHandler := handlers.NewAccountHandler(application.NewAccountService(db, pool, mailService))
	emailHandler := handlers.NewEmailHandler(mailService)
	telegramHandler := handlers.NewTelegramHandler(application.NewTelegramService(db, mailService, telegramClient))
	authHandler := handlers.NewAuthHandler(instanceAccess)
	auth := r.Group("/api/auth")
	{
		auth.GET("/status", authHandler.Status)
		auth.POST("/login", authHandler.Login)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(instanceAccess))
	{

		api.GET("/providers", accountHandler.ListProviders)
		api.GET("/accounts", accountHandler.ListAccounts)
		api.POST("/accounts", accountHandler.AddAccount)
		api.PUT("/accounts/:id", accountHandler.UpdateAccount)
		api.DELETE("/accounts/:id", accountHandler.DeleteAccount)
		api.POST("/accounts/:id/test", accountHandler.TestAccount)
		api.POST("/accounts/:id/refresh", emailHandler.RefreshAccountEmails)

		api.GET("/emails", emailHandler.ListEmails)
		api.POST("/emails/refresh", emailHandler.RefreshEmails)
		api.GET("/emails/:id", emailHandler.GetEmail)
		api.GET("/telegram/config", telegramHandler.GetTelegramConfig)
		api.PUT("/telegram/config", telegramHandler.UpdateTelegramConfig)
		api.POST("/telegram/test", telegramHandler.TestTelegramConnection)
		api.POST("/telegram/send/:id", telegramHandler.SendEmailToTelegram)
		api.GET("/telegram/tasks", telegramHandler.ListTasks)
		api.POST("/telegram/tasks/:id/retry", telegramHandler.RetryTask)
		api.DELETE("/telegram/tasks/:id", telegramHandler.DeleteTask)
	}

	r.GET("/health", health(db))

	return r
}
