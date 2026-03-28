package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/chat-diploma/variant-b/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const messagesCollection = "messages"

// MessageRepository handles MongoDB persistence for chat messages.
type MessageRepository struct {
	col *mongo.Collection
}

// NewMessageRepository creates a MessageRepository backed by the given database.
func NewMessageRepository(db *mongo.Database) *MessageRepository {
	return &MessageRepository{col: db.Collection(messagesCollection)}
}

// EnsureIndexes creates the required indexes on the messages collection.
// It is safe to call multiple times (createIndex is idempotent).
func (r *MessageRepository) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "room_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("room_id_created_at"),
		},
		{
			Keys:    bson.D{{Key: "message_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("message_id_unique"),
		},
	}

	_, err := r.col.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("message repository: ensure indexes: %w", err)
	}
	return nil
}

// Insert upserts a message by message_id to guarantee idempotency.
// If the document already exists the operation is a no-op.
func (r *MessageRepository) Insert(ctx context.Context, msg *model.Message) error {
	filter := bson.M{"message_id": msg.MessageID}
	update := bson.M{
		"$setOnInsert": bson.M{
			"message_id": msg.MessageID,
			"room_id":    msg.RoomID,
			"sender":     msg.Sender,
			"content":    msg.Content,
			"created_at": msg.CreatedAt,
			"delivered":  msg.Delivered,
		},
	}
	opts := options.Update().SetUpsert(true)

	_, err := r.col.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("message repository: insert: %w", err)
	}
	return nil
}

// ListByRoom returns messages for roomID using cursor-based pagination.
// before is an opaque cursor that encodes the created_at time of the last seen
// message (RFC3339Nano string). Pass "" to fetch the latest page.
// Returns the messages (oldest-first within the page), a hasMore flag, and any error.
func (r *MessageRepository) ListByRoom(ctx context.Context, roomID, before string, limit int) ([]model.Message, bool, error) {
	if limit <= 0 {
		limit = 50
	}

	filter := bson.M{"room_id": roomID}
	if before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			return nil, false, fmt.Errorf("message repository: parse cursor: %w", err)
		}
		filter["created_at"] = bson.M{"$lt": t}
	}

	// Fetch limit+1 to determine hasMore.
	fetchLimit := int64(limit + 1)
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(fetchLimit)

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, fmt.Errorf("message repository: find: %w", err)
	}
	defer cursor.Close(ctx)

	var raw []model.Message
	if err := cursor.All(ctx, &raw); err != nil {
		return nil, false, fmt.Errorf("message repository: decode: %w", err)
	}

	hasMore := len(raw) > limit
	if hasMore {
		raw = raw[:limit]
	}

	// Reverse so result is oldest-first within the page.
	for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
		raw[i], raw[j] = raw[j], raw[i]
	}

	return raw, hasMore, nil
}
