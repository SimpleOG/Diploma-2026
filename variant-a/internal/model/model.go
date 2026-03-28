package model

import "time"

// User represents a registered user.
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// Room represents a chat room.
type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// RoomMember represents a user's membership in a room.
type RoomMember struct {
	RoomID   string    `json:"room_id"`
	UserID   string    `json:"user_id"`
	JoinedAt time.Time `json:"joined_at"`
}

// Message represents a chat message.
type Message struct {
	ID             string    `json:"id"`
	RoomID         string    `json:"room_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// ──────────────────────────────────────────────
// Request / Response DTOs
// ──────────────────────────────────────────────

// RegisterRequest is the payload for POST /api/v1/auth/register.
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

// LoginRequest is the payload for POST /api/v1/auth/login.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse is returned after successful register or login.
type AuthResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// CreateRoomRequest is the payload for POST /api/v1/rooms.
type CreateRoomRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

// RoomResponse extends Room with the current member count.
type RoomResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	MemberCount int       `json:"member_count"`
}

// MessageResponse is the DTO returned for paginated message lists.
type MessageResponse struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	// NextCursor is the ID of the oldest message in the current page, used as
	// the "before" cursor for the next request.
	NextCursor string `json:"next_cursor,omitempty"`
}
