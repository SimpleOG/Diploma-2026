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
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/chat-diploma/variant-c/messages-service/internal/config"
	"github.com/chat-diploma/variant-c/messages-service/internal/handler"
	"github.com/chat-diploma/variant-c/messages-service/internal/middleware"
	"github.com/chat-diploma/variant-c/messages-service/internal/rabbitmq"
	"github.com/chat-diploma/variant-c/messages-service/internal/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Connect to MongoDB.
	mongoClient, err := connectMongo(cfg.MongoURI)
	if err != nil {
		slog.Error("failed to connect to MongoDB", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = mongoClient.Disconnect(ctx)
	}()
	slog.Info("connected to MongoDB")

	mongoDB := mongoClient.Database(cfg.MongoDB)
	msgRepo := repository.NewMessageRepository(mongoDB)

	// Ensure MongoDB indexes.
	indexCtx, indexCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer indexCancel()
	if err := msgRepo.EnsureIndexes(indexCtx); err != nil {
		slog.Error("failed to ensure MongoDB indexes", "error", err)
		os.Exit(1)
	}
	slog.Info("MongoDB indexes ensured")

	// Connect to RabbitMQ.
	publisher, err := connectRabbitMQ(cfg.RabbitMQURL)
	if err != nil {
		slog.Error("failed to connect to RabbitMQ", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()
	slog.Info("connected to RabbitMQ")

	// Initialize handlers.
	msgHandler := handler.NewMessageHandler(msgRepo, publisher, cfg.RoomsServiceURL)
	healthHandler := handler.NewHealthHandler(mongoClient)

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

	authMw := middleware.AuthMiddleware(cfg.AuthServiceURL)

	v1 := r.Group("/api/v1")
	v1.Use(authMw)
	{
		v1.POST("/messages", msgHandler.SendMessage)
		v1.GET("/rooms/:room_id/messages", msgHandler.ListMessages)
	}

	r.GET("/health", healthHandler.Health)

	// Start HTTP server.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting messages-service", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down messages-service...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("messages-service stopped")
}

func connectMongo(uri string) (*mongo.Client, error) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI(uri).SetServerAPIOptions(serverAPI)

	var client *mongo.Client
	var err error

	for i := 0; i < 10; i++ {
		client, err = mongo.Connect(context.Background(), opts)
		if err != nil {
			slog.Warn("failed to create MongoDB client", "attempt", i+1, "error", err)
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = client.Ping(pingCtx, readpref.Primary())
		pingCancel()
		if err == nil {
			return client, nil
		}

		_ = client.Disconnect(context.Background())
		slog.Warn("waiting for MongoDB", "attempt", i+1, "error", err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("could not connect to MongoDB: %w", err)
}

func connectRabbitMQ(url string) (*rabbitmq.Publisher, error) {
	var publisher *rabbitmq.Publisher
	var err error

	for i := 0; i < 10; i++ {
		publisher, err = rabbitmq.NewPublisher(url)
		if err == nil {
			return publisher, nil
		}
		slog.Warn("waiting for RabbitMQ", "attempt", i+1, "error", err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("could not connect to RabbitMQ: %w", err)
}
