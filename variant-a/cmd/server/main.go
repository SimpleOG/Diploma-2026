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

	"github.com/chat-diploma/variant-a/internal/auth"
	"github.com/chat-diploma/variant-a/internal/config"
	"github.com/chat-diploma/variant-a/internal/handler"
	"github.com/chat-diploma/variant-a/internal/middleware"
	"github.com/chat-diploma/variant-a/internal/repository"
	appws "github.com/chat-diploma/variant-a/internal/websocket"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func main() {
	// ── Structured logger ────────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Configuration ────────────────────────────────────────────────────────
	cfg := config.Load()

	// ── PostgreSQL ───────────────────────────────────────────────────────────
	db, err := sql.Open("postgres", cfg.DBDSN)
	if err != nil {
		slog.Error("failed to open postgres", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("db close error", "err", err)
		}
	}()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Ping with retries to handle startup ordering in docker-compose.
	if err := waitForDB(db, 30*time.Second); err != nil {
		slog.Error("postgres not ready", "err", err)
		os.Exit(1)
	}
	slog.Info("connected to postgres")

	// ── Migrations ───────────────────────────────────────────────────────────
	if err := runMigrations(db); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	// ── Redis ────────────────────────────────────────────────────────────────
	redisOpts, err := redis.ParseURL(fmt.Sprintf("redis://%s", cfg.RedisAddr))
	if err != nil {
		// Fallback to simple options if parsing fails.
		redisOpts = &redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		}
	} else {
		if cfg.RedisPassword != "" {
			redisOpts.Password = cfg.RedisPassword
		}
	}

	redisClient := redis.NewClient(redisOpts)
	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Warn("redis close error", "err", err)
		}
	}()

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to redis", "err", err)
		os.Exit(1)
	}
	slog.Info("connected to redis")

	// ── Repositories ─────────────────────────────────────────────────────────
	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)
	msgRepo := repository.NewMessageRepository(db)

	// ── Services ─────────────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpirationHrs)

	// ── WebSocket Hub ─────────────────────────────────────────────────────────
	hub := appws.NewHub(redisClient)
	go hub.Run()

	// ── Message Handler ───────────────────────────────────────────────────────
	msgHandler := appws.NewMessageHandler(msgRepo, roomRepo, hub, redisClient)

	// ── HTTP Handlers ─────────────────────────────────────────────────────────
	authHandler := handler.NewAuthHandler(userRepo, authSvc)
	roomHandler := handler.NewRoomHandler(roomRepo, msgRepo)
	healthHandler := handler.NewHealthHandler(db, redisClient)

	// ── Gin Router ────────────────────────────────────────────────────────────
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Global middleware.
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// ── Routes ────────────────────────────────────────────────────────────────
	r.GET("/health", healthHandler.Health)

	v1 := r.Group("/api/v1")
	{
		authGroup := v1.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
			authGroup.POST("/login", authHandler.Login)
		}

		rooms := v1.Group("/rooms")
		rooms.Use(middleware.Auth(authSvc))
		{
			rooms.GET("", roomHandler.ListRooms)
			rooms.POST("", roomHandler.CreateRoom)
			rooms.POST("/:id/join", roomHandler.JoinRoom)
			rooms.GET("/:id/messages", roomHandler.GetMessages)
		}
	}

	// WebSocket endpoint.
	r.GET("/ws", middleware.WebSocketAuth(authSvc), appws.ServeWS(hub, msgHandler))

	// ── HTTP Server with graceful shutdown ────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background goroutine.
	go func() {
		slog.Info("server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	// Give active connections 10 seconds to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced shutdown", "err", err)
	}

	slog.Info("server stopped")
}

// waitForDB retries db.Ping until it succeeds or the timeout is reached.
func waitForDB(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := db.Ping(); err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return db.Ping()
}

// runMigrations applies the schema using direct SQL execution.
// Using inline SQL avoids any dependency on file paths or embedded FS.
func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username   VARCHAR(64) NOT NULL UNIQUE,
    password   VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rooms (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id    UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content    TEXT NOT NULL CHECK (char_length(content) > 0 AND char_length(content) <= 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_room_created ON messages(room_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_room_members_user ON room_members(user_id);
`)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}
