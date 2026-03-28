package model

import "time"

// Room represents a chat room.
type Room struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	OwnerID   string    `json:"owner_id" db:"owner_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// RoomMember represents a user's membership in a room.
type RoomMember struct {
	RoomID   string    `json:"room_id" db:"room_id"`
	UserID   string    `json:"user_id" db:"user_id"`
	JoinedAt time.Time `json:"joined_at" db:"joined_at"`
}

// CreateRoomRequest is the payload for creating a new room.
type CreateRoomRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

// RoomResponse is the external representation of a room with member count.
type RoomResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OwnerID     string    `json:"owner_id"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// JoinResponse is returned when a user joins a room.
type JoinResponse struct {
	RoomID  string `json:"room_id"`
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}
