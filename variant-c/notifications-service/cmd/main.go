package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/chat-diploma/variant-c/notifications-service/internal/config"
	"github.com/chat-diploma/variant-c/notifications-service/internal/handler"
	"github.com/chat-diploma/variant-c/notifications-service/internal/middleware"
	"github.com/chat-diploma/variant-c/notifications-service/internal/rabbitmq"
	ws "github.com/chat-diploma/variant-c/notifications-service/internal/websocket"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Connect to Redis.
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		cancel()
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	cancel()
	slog.Info("connected to Redis")

	// Create and start WebSocket hub.
	hub := ws.NewHub(redisClient)
	hubCtx, hubCancel := context.WithCancel(context.Background())
	go hub.Run(hubCtx)

	// Connect to RabbitMQ and start consumer.
	consumer, err := connectRabbitMQ(cfg.RabbitMQURL, hub)
	if err != nil {
		slog.Error("failed to connect to RabbitMQ", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	go consumer.Start(consumerCtx)
	slog.Info("RabbitMQ consumer started")

	// Setup Gin.
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Routes.
	wsHandler := handler.NewWSHandler(hub)
	wsMw := middleware.WSAuth(cfg.AuthServiceURL)

	r.GET("/health", handler.Health)
	r.GET("/ws", wsMw, wsHandler.ServeWS)

	// Start HTTP server.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting notifications-service", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down notifications-service...")

	// Stop accepting new connections.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}

	// Close WebSocket connections with code 1001 (going away).
	slog.Info("closing websocket connections")

	// Stop hub and consumer.
	hubCancel()
	consumerCancel()

	_ = redisClient.Close()

	slog.Info("notifications-service stopped")
}

func connectRabbitMQ(url string, broadcaster rabbitmq.Broadcaster) (*rabbitmq.Consumer, error) {
	var consumer *rabbitmq.Consumer
	var err error

	for i := 0; i < 10; i++ {
		consumer, err = rabbitmq.NewConsumer(url, broadcaster)
		if err == nil {
			return consumer, nil
		}
		slog.Warn("waiting for RabbitMQ", "attempt", i+1, "error", err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("could not connect to RabbitMQ: %w", err)
}
