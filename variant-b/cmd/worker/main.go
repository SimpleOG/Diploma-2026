package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/chat-diploma/variant-b/internal/config"
	kafkapkg "github.com/chat-diploma/variant-b/internal/kafka"
	"github.com/chat-diploma/variant-b/internal/model"
	"github.com/chat-diploma/variant-b/internal/repository"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

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

	// Ensure indexes on startup.
	idxCtx, idxCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer idxCancel()
	if err := messageRepo.EnsureIndexes(idxCtx); err != nil {
		slog.Error("ensure mongodb indexes", "error", err)
		os.Exit(1)
	}

	// ── Graceful shutdown context ─────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Kafka Consumers ───────────────────────────────────────────────────────
	workerInstances := cfg.WorkerInstances
	if workerInstances < 1 {
		workerInstances = 4
	}

	slog.Info("starting worker instances", "count", workerInstances)

	var wg sync.WaitGroup
	for i := 0; i < workerInstances; i++ {
		wg.Add(1)
		go func(instanceID int) {
			defer wg.Done()
			runWorker(ctx, cfg, instanceID, messageRepo)
		}(i)
	}

	wg.Wait()
	slog.Info("all workers stopped")
}

// runWorker creates a Kafka consumer and processes messages until ctx is cancelled.
func runWorker(ctx context.Context, cfg *config.Config, instanceID int, messageRepo *repository.MessageRepository) {
	consumerGroup := cfg.KafkaConsumerGroup

	consumer, err := kafkapkg.NewConsumer(cfg.KafkaBrokers, consumerGroup, cfg.KafkaTopic)
	if err != nil {
		slog.Error("create kafka consumer", "instance", instanceID, "error", err)
		return
	}
	defer consumer.Close()

	slog.Info("worker started", "instance", instanceID, "group", consumerGroup, "topic", cfg.KafkaTopic)

	consumer.Consume(ctx, func(km *model.KafkaMessage) error {
		return persistMessage(ctx, messageRepo, km)
	})

	slog.Info("worker stopped", "instance", instanceID)
}

// persistMessage upserts a Kafka message into MongoDB.
func persistMessage(ctx context.Context, repo *repository.MessageRepository, km *model.KafkaMessage) error {
	msg := &model.Message{
		MessageID: km.MessageID,
		RoomID:    km.RoomID,
		Sender: model.Sender{
			ID:       km.SenderID,
			Username: km.SenderUsername,
		},
		Content:   km.Content,
		CreatedAt: km.CreatedAt,
		Delivered: true,
	}

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := repo.Insert(writeCtx, msg); err != nil {
		slog.Error("persist message", "message_id", km.MessageID, "error", err)
		return err
	}

	slog.Debug("message persisted", "message_id", km.MessageID, "room_id", km.RoomID)
	return nil
}
