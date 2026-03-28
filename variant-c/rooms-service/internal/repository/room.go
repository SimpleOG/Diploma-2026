package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-c/rooms-service/internal/model"
)

// ErrNotFound is returned when a room is not found.
var ErrNotFound = errors.New("room not found")

// ErrAlreadyMember is returned when a user is already a member of the room.
var ErrAlreadyMember = errors.New("user is already a member")

// RoomRepository handles room persistence.
type RoomRepository struct {
	db *sql.DB
}

// NewRoomRepository creates a new RoomRepository.
func NewRoomRepository(db *sql.DB) *RoomRepository {
	return &RoomRepository{db: db}
}

// Create inserts a new room into the database.
func (r *RoomRepository) Create(name, ownerID string) (*model.Room, error) {
	const q = `
		INSERT INTO rooms (name, owner_id)
		VALUES ($1, $2)
		RETURNING id, name, owner_id, created_at
	`
	room := &model.Room{}
	err := r.db.QueryRow(q, name, ownerID).Scan(
		&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create room: %w", err)
	}

	// Automatically add owner as a member.
	if err := r.addMemberTx(room.ID, ownerID); err != nil {
		return nil, fmt.Errorf("add owner as member: %w", err)
	}

	return room, nil
}

// addMemberTx adds a member without duplicate check (internal use).
func (r *RoomRepository) addMemberTx(roomID, userID string) error {
	const q = `
		INSERT INTO room_members (room_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (room_id, user_id) DO NOTHING
	`
	_, err := r.db.Exec(q, roomID, userID)
	return err
}

// List returns all rooms with their member counts.
func (r *RoomRepository) List() ([]model.RoomResponse, error) {
	const q = `
		SELECT r.id, r.name, r.owner_id, r.created_at, COUNT(rm.user_id) AS member_count
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id
		GROUP BY r.id, r.name, r.owner_id, r.created_at
		ORDER BY r.created_at DESC
	`
	rows, err := r.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []model.RoomResponse
	for rows.Next() {
		var room model.RoomResponse
		if err := rows.Scan(&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt, &room.MemberCount); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}
	if rooms == nil {
		rooms = []model.RoomResponse{}
	}
	return rooms, nil
}

// GetByID retrieves a single room by ID.
func (r *RoomRepository) GetByID(roomID string) (*model.Room, error) {
	const q = `SELECT id, name, owner_id, created_at FROM rooms WHERE id = $1`
	room := &model.Room{}
	err := r.db.QueryRow(q, roomID).Scan(&room.ID, &room.Name, &room.OwnerID, &room.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get room by id: %w", err)
	}
	return room, nil
}

// AddMember adds a user to a room. Returns ErrAlreadyMember if already joined.
func (r *RoomRepository) AddMember(roomID, userID string) error {
	const q = `
		INSERT INTO room_members (room_id, user_id)
		VALUES ($1, $2)
	`
	_, err := r.db.Exec(q, roomID, userID)
	if err != nil {
		if isDuplicateError(err) {
			return ErrAlreadyMember
		}
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

// IsMember checks whether a user is a member of the room.
func (r *RoomRepository) IsMember(roomID, userID string) (bool, error) {
	const q = `SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2 LIMIT 1`
	var dummy int
	err := r.db.QueryRow(q, roomID, userID).Scan(&dummy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check membership: %w", err)
	}
	return true, nil
}

// GetMembersCount returns the number of members in a room.
func (r *RoomRepository) GetMembersCount(roomID string) (int, error) {
	const q = `SELECT COUNT(*) FROM room_members WHERE room_id = $1`
	var count int
	if err := r.db.QueryRow(q, roomID).Scan(&count); err != nil {
		return 0, fmt.Errorf("get members count: %w", err)
	}
	return count, nil
}

// isDuplicateError checks if the error is a PostgreSQL unique constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return containsStr(s, "23505") || containsStr(s, "duplicate key")
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
