package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/chat-diploma/variant-a/internal/model"
)

// MessageRepository handles persistence for chat messages.
type MessageRepository struct {
	db *sql.DB
}

// NewMessageRepository creates a new MessageRepository.
func NewMessageRepository(db *sql.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// Create inserts a new message and returns the created record including the
// sender's username via a join.
func (r *MessageRepository) Create(ctx context.Context, roomID, senderID, content string) (*model.Message, error) {
	const query = `
		WITH ins AS (
			INSERT INTO messages (room_id, sender_id, content)
			VALUES ($1, $2, $3)
			RETURNING id, room_id, sender_id, content, created_at
		)
		SELECT ins.id, ins.room_id, ins.sender_id, u.username, ins.content, ins.created_at
		FROM ins
		JOIN users u ON u.id = ins.sender_id`

	msg := &model.Message{}
	err := r.db.QueryRowContext(ctx, query, roomID, senderID, content).Scan(
		&msg.ID, &msg.RoomID, &msg.SenderID, &msg.SenderUsername, &msg.Content, &msg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("MessageRepository.Create: %w", err)
	}

	return msg, nil
}

// ListByRoom returns up to limit+1 messages for cursor-based pagination.
// When before is non-empty it is treated as a message ID; only messages older
// than that message are returned.
// Returns the messages (newest-first), a hasMore flag, and any error.
func (r *MessageRepository) ListByRoom(
	ctx context.Context,
	roomID string,
	before string,
	limit int,
) ([]model.Message, bool, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		rows *sql.Rows
		err  error
	)

	// Fetch limit+1 rows so we can detect whether there are more pages.
	if before == "" {
		const query = `
			SELECT m.id, m.room_id, m.sender_id, u.username, m.content, m.created_at
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.room_id = $1
			ORDER BY m.created_at DESC
			LIMIT $2`
		rows, err = r.db.QueryContext(ctx, query, roomID, limit+1)
	} else {
		// Use a sub-select to find the created_at of the cursor message so we
		// can do a stable keyset-based comparison.
		const query = `
			SELECT m.id, m.room_id, m.sender_id, u.username, m.content, m.created_at
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.room_id = $1
			  AND m.created_at < (SELECT created_at FROM messages WHERE id = $2)
			ORDER BY m.created_at DESC
			LIMIT $3`
		rows, err = r.db.QueryContext(ctx, query, roomID, before, limit+1)
	}

	if err != nil {
		return nil, false, fmt.Errorf("MessageRepository.ListByRoom: %w", err)
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var msg model.Message
		if err := rows.Scan(
			&msg.ID, &msg.RoomID, &msg.SenderID, &msg.SenderUsername, &msg.Content, &msg.CreatedAt,
		); err != nil {
			return nil, false, fmt.Errorf("MessageRepository.ListByRoom scan: %w", err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("MessageRepository.ListByRoom rows: %w", err)
	}

	hasMore := false
	if len(messages) > limit {
		hasMore = true
		messages = messages[:limit]
	}

	return messages, hasMore, nil
}
