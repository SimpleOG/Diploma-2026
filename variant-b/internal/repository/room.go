package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-b/internal/model"
)

// RoomRepository handles PostgreSQL persistence for rooms and membership.
type RoomRepository struct {
	db *sql.DB
}

// NewRoomRepository creates a new RoomRepository.
func NewRoomRepository(db *sql.DB) *RoomRepository {
	return &RoomRepository{db: db}
}

// Create inserts a new room and immediately adds the owner as a member.
func (r *RoomRepository) Create(ctx context.Context, name, ownerID string) (*model.Room, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("repository: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const insertRoom = `
		INSERT INTO rooms (name, owner_id)
		VALUES ($1, $2)
		RETURNING id, name, owner_id, created_at`

	var room model.Room
	if err = tx.QueryRowContext(ctx, insertRoom, name, ownerID).
		Scan(&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt); err != nil {
		return nil, fmt.Errorf("repository: create room: %w", err)
	}

	const insertMember = `INSERT INTO room_members (room_id, user_id) VALUES ($1, $2)`
	if _, err = tx.ExecContext(ctx, insertMember, room.ID, ownerID); err != nil {
		return nil, fmt.Errorf("repository: add owner as member: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("repository: commit create room: %w", err)
	}
	return &room, nil
}

// List returns all rooms ordered by creation time descending.
func (r *RoomRepository) List(ctx context.Context) ([]model.Room, error) {
	const q = `SELECT id, name, owner_id, created_at FROM rooms ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("repository: list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []model.Room
	for rows.Next() {
		var rm model.Room
		if err := rows.Scan(&rm.ID, &rm.Name, &rm.OwnerID, &rm.CreatedAt); err != nil {
			return nil, fmt.Errorf("repository: scan room: %w", err)
		}
		rooms = append(rooms, rm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: rows error: %w", err)
	}
	return rooms, nil
}

// GetByID fetches a room by its primary key.
func (r *RoomRepository) GetByID(ctx context.Context, id string) (*model.Room, error) {
	const q = `SELECT id, name, owner_id, created_at FROM rooms WHERE id = $1`

	row := r.db.QueryRowContext(ctx, q, id)
	var rm model.Room
	if err := row.Scan(&rm.ID, &rm.Name, &rm.OwnerID, &rm.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: get room by id: %w", err)
	}
	return &rm, nil
}

// AddMember inserts a membership row, ignoring conflicts (idempotent).
func (r *RoomRepository) AddMember(ctx context.Context, roomID, userID string) error {
	const q = `
		INSERT INTO room_members (room_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`

	if _, err := r.db.ExecContext(ctx, q, roomID, userID); err != nil {
		return fmt.Errorf("repository: add member: %w", err)
	}
	return nil
}

// IsMember reports whether userID belongs to roomID.
func (r *RoomRepository) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	const q = `
		SELECT 1 FROM room_members
		WHERE room_id = $1 AND user_id = $2
		LIMIT 1`

	row := r.db.QueryRowContext(ctx, q, roomID, userID)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("repository: is member: %w", err)
	}
	return true, nil
}
