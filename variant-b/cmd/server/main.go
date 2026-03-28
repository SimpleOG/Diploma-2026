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

	gincors "github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/chat-diploma/variant-b/internal/auth"
	"github.com/chat-diploma/variant-b/internal/cache"
	"github.com/chat-diploma/variant-b/internal/config"
	"github.com/chat-diploma/variant-b/internal/handler"
	kafkapkg "github.com/chat-diploma/variant-b/internal/kafka"
	"github.com/chat-diploma/variant-b/internal/middleware"
	"github.com/chat-diploma/variant-b/internal/repository"
	wshub "github.com/chat-diploma/variant-b/internal/websocket"
)

func main() {
	// Load .env if present (ignore error in production).
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	// ── PostgreSQL ────────────────────────────────────────────────────────────
	db, err := sql.Open("postgres", cfg.DBDSN)
	if err != nil {
		slog.Error("open postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := waitForDB(db, 30*time.Second); err != nil {
		slog.Error("postgres not ready", "error", err)
		os.Exit(1)
	}

	if err := runMigrations(db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	// ── MongoDB ───────────────────────────────────────────────────────────────
	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mongoCancel()

	mongoClient, err := mongo.Connect(mongoCtx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		slog.Error("connect mongodb", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := mongoClient.Disconnect(shutCtx); err != nil {
			slog.Error("disconnect mongodb", "error", err)
		}
	}()

	mongoDB := mongoClient.Database(cfg.MongoDB)
	messageRepo := repository.NewMessageRepository(mongoDB)

	idxCtx, idxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer idxCancel()
	if err := messageRepo.EnsureIndexes(idxCtx); err != nil {
		slog.Error("ensure mongodb indexes", "error", err)
		os.Exit(1)
	}

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	defer rdb.Close()

	redisCtx, redisCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisCancel()
	if err := rdb.Ping(redisCtx).Err(); err != nil {
		slog.Warn("redis ping failed – continuing", "error", err)
	} else {
		slog.Info("redis connected", "addr", cfg.RedisAddr)
	}

	// ── Memcached ─────────────────────────────────────────────────────────────
	memcached := cache.NewMemcachedClient(cfg.MemcachedAddr)
	slog.Info("memcached configured", "addr", cfg.MemcachedAddr)

	// ── Kafka Producer ────────────────────────────────────────────────────────
	producer, err := kafkapkg.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		slog.Error("create kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	// ── Repositories ──────────────────────────────────────────────────────────
	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)

	// ── Auth Service ──────────────────────────────────────────────────────────
	authSvc := auth.NewService(cfg.JWTSecret, cfg.JWTExpirationHours)

	// ── WebSocket Hub ─────────────────────────────────────────────────────────
	hub := wshub.NewHub()
	go hub.Run()

	wsHandler := wshub.NewHandler(hub, roomRepo, memcached, producer, cfg.KafkaTopic)

	// ── HTTP Handlers ─────────────────────────────────────────────────────────
	authHandler := handler.NewAuthHandler(userRepo, authSvc)
	roomHandler := handler.NewRoomHandler(roomRepo, messageRepo)
	healthHandler := handler.NewHealthHandler(db, mongoDB)

	// ── Gin Router ────────────────────────────────────────────────────────────
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(gincors.New(gincors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
	}))

	r.GET("/health", healthHandler.Health)

	api := r.Group("/api/v1")
	{
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
			authGroup.POST("/login", authHandler.Login)
		}

		roomGroup := api.Group("/rooms")
		roomGroup.Use(middleware.Auth(authSvc))
		{
			roomGroup.POST("", roomHandler.Create)
			roomGroup.GET("", roomHandler.List)
			roomGroup.POST("/:id/join", roomHandler.Join)
			roomGroup.GET("/:id/messages", roomHandler.Messages)
		}
	}

	// WebSocket endpoint – auth middleware reads the token from query param.
	r.GET("/ws", middleware.Auth(authSvc), wsHandler.ServeWS)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")
}

// waitForDB retries pinging the database until it responds or the timeout elapses.
func waitForDB(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := db.Ping(); err == nil {
			slog.Info("postgres connected")
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("postgres did not become ready within %s", timeout)
}

// runMigrations executes the SQL migration file directly.
func runMigrations(db *sql.DB) error {
	migration := `
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    username   VARCHAR(64)  NOT NULL UNIQUE,
    password   VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rooms (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    owner_id   UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id   UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_room_members_user ON room_members(user_id);
`
	if _, err := db.Exec(migration); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	slog.Info("migrations applied")
	return nil
}
