package repository

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/chat-diploma/variant-c/messages-service/internal/model"
)

const collectionName = "messages"

// MessageRepository handles message persistence in MongoDB.
type MessageRepository struct {
	col *mongo.Collection
}

// NewMessageRepository creates a new MessageRepository.
func NewMessageRepository(db *mongo.Database) *MessageRepository {
	return &MessageRepository{col: db.Collection(collectionName)}
}

// EnsureIndexes creates required indexes for the messages collection.
func (r *MessageRepository) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "room_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("idx_room_id_created_at"),
		},
		{
			Keys:    bson.D{{Key: "message_id", Value: 1}},
			Options: options.Index().SetName("idx_message_id").SetUnique(true),
		},
	}

	_, err := r.col.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}
	return nil
}

// Insert saves a new message to MongoDB.
func (r *MessageRepository) Insert(ctx context.Context, msg *model.Message) error {
	_, err := r.col.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// ListByRoom retrieves messages for a room using cursor-based pagination.
// beforeTime, if non-zero, fetches messages created before that time (for pagination).
// limit controls how many messages to return (max 100).
func (r *MessageRepository) ListByRoom(ctx context.Context, roomID string, beforeTime time.Time, limit int64) ([]model.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	filter := bson.M{"room_id": roomID}
	if !beforeTime.IsZero() {
		filter["created_at"] = bson.M{"$lt": beforeTime}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(limit)

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find messages: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []model.Message
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}

	// Reverse to return chronological order (oldest first).
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	if messages == nil {
		messages = []model.Message{}
	}
	return messages, nil
}
