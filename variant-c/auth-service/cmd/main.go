package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"

	"github.com/chat-diploma/variant-c/auth-service/internal/config"
	"github.com/chat-diploma/variant-c/auth-service/internal/handler"
	"github.com/chat-diploma/variant-c/auth-service/internal/middleware"
	"github.com/chat-diploma/variant-c/auth-service/internal/repository"
)

func main() {
	// Initialize structured logger.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Connect to PostgreSQL.
	db, err := openDB(cfg.DBDSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("connected to PostgreSQL")

	// Run migrations.
	if err := runMigrations(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	// Initialize repositories.
	userRepo := repository.NewUserRepository(db)

	// Initialize handlers.
	authHandler := handler.NewAuthHandler(userRepo, cfg.JWTSecret, cfg.JWTExpirationHrs)

	// Setup Gin.
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// Public routes.
	v1 := r.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
	}

	// Internal routes (no JWT middleware - called by other services).
	internal := r.Group("/internal")
	{
		internal.POST("/auth/validate", authHandler.ValidateToken)
	}

	// Health check.
	r.GET("/health", func(c *gin.Context) {
		if err := db.PingContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Start HTTP server.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting auth-service", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down auth-service...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("auth-service stopped")
}

// openDB opens a PostgreSQL connection with retries.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Retry connection up to 10 times.
	for i := 0; i < 10; i++ {
		if err = db.Ping(); err == nil {
			return db, nil
		}
		slog.Warn("waiting for database", "attempt", i+1, "error", err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("could not connect to database: %w", err)
}

// runMigrations applies the schema via direct SQL execution.
func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE TABLE IF NOT EXISTS users (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username   VARCHAR(64) NOT NULL UNIQUE,
    password   VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
