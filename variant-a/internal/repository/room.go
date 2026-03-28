package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/lib/pq"
)

// ErrDuplicate is returned when an insert violates a unique/primary-key constraint.
var ErrDuplicate = errors.New("duplicate")

// RoomRepository handles persistence for rooms and memberships.
type RoomRepository struct {
	db *sql.DB
}

// NewRoomRepository creates a new RoomRepository.
func NewRoomRepository(db *sql.DB) *RoomRepository {
	return &RoomRepository{db: db}
}

// Create inserts a new room and returns the created record.
func (r *RoomRepository) Create(ctx context.Context, name, ownerID string) (*model.Room, error) {
	const query = `
		INSERT INTO rooms (name, owner_id)
		VALUES ($1, $2)
		RETURNING id, created_at`

	room := &model.Room{
		Name:    name,
		OwnerID: ownerID,
	}

	err := r.db.QueryRowContext(ctx, query, name, ownerID).Scan(&room.ID, &room.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("RoomRepository.Create: %w", err)
	}

	return room, nil
}

// List returns all rooms with member counts, ordered by creation time descending.
func (r *RoomRepository) List(ctx context.Context) ([]model.RoomResponse, error) {
	const query = `
		SELECT r.id, r.name, r.owner_id, r.created_at, COUNT(rm.user_id) AS member_count
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id
		GROUP BY r.id
		ORDER BY r.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("RoomRepository.List: %w", err)
	}
	defer rows.Close()

	var rooms []model.RoomResponse
	for rows.Next() {
		var room model.RoomResponse
		if err := rows.Scan(&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt, &room.MemberCount); err != nil {
			return nil, fmt.Errorf("RoomRepository.List scan: %w", err)
		}
		rooms = append(rooms, room)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("RoomRepository.List rows: %w", err)
	}

	return rooms, nil
}

// GetByID returns a room by its UUID.
func (r *RoomRepository) GetByID(ctx context.Context, id string) (*model.Room, error) {
	const query = `
		SELECT id, name, owner_id, created_at
		FROM rooms
		WHERE id = $1`

	room := &model.Room{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("RoomRepository.GetByID: %w", err)
	}

	return room, nil
}

// AddMember inserts a room_members row. Returns ErrDuplicate if already a member.
func (r *RoomRepository) AddMember(ctx context.Context, roomID, userID string) error {
	const query = `
		INSERT INTO room_members (room_id, user_id)
		VALUES ($1, $2)`

	_, err := r.db.ExecContext(ctx, query, roomID, userID)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			return ErrDuplicate
		}
		return fmt.Errorf("RoomRepository.AddMember: %w", err)
	}

	return nil
}

// IsMember checks whether userID is a member of roomID.
func (r *RoomRepository) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	const query = `
		SELECT 1 FROM room_members
		WHERE room_id = $1 AND user_id = $2
		LIMIT 1`

	var dummy int
	err := r.db.QueryRowContext(ctx, query, roomID, userID).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("RoomRepository.IsMember: %w", err)
	}

	return true, nil
}
