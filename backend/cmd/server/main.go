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
	"syscall"
	"time"

	"one-mail/backend/config"
	"one-mail/backend/database"
	"one-mail/backend/internal/access"
	"one-mail/backend/internal/handlers"
	"one-mail/backend/internal/middleware"
	"one-mail/backend/internal/services/imap"

	"github.com/gin-gonic/gin"
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
	if err := database.InitDatabase(); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		return fmt.Errorf("get database pool: %w", err)
	}
	defer sqlDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	statsCtx, cancelStats := context.WithCancel(context.Background())
	defer cancelStats()
	go logResourceStats(statsCtx, sqlDB)
	server := newHTTPServer(fmt.Sprintf(":%d", cfg.Server.Port), buildRouter(instanceAccess))
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

func logResourceStats(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := db.Stats()
			imapStats := imap.GetConnectionPool().GetStats()
			log.Printf("event=resource_stats db_open=%d db_in_use=%d db_idle=%d db_wait_count=%d db_wait_duration_ms=%d imap_connections=%v imap_wait_count=%v imap_wait_duration_ms=%v",
				stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount, stats.WaitDuration.Milliseconds(), imapStats["total_connections"], imapStats["wait_count"], imapStats["wait_duration_ms"])
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

func health(c *gin.Context) {
	if database.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	db, err := database.DB.DB()
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()
		err = db.PingContext(ctx)
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func buildRouter(instanceAccess *access.Access) *gin.Engine {
	r := gin.New()
	r.Use(requestLogger())
	r.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ interface{}) {
		log.Print("event=http_panic")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}))
	r.Use(requestLimits())
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
		api.GET("/telegram/config", handlers.GetTelegramConfig)
		api.PUT("/telegram/config", handlers.UpdateTelegramConfig)
		api.POST("/telegram/test", handlers.TestTelegramConnection)
		api.POST("/telegram/send/:id", handlers.SendEmailToTelegram)
	}

	r.GET("/health", health)

	return r
}
