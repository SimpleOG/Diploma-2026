package model

import "time"

// ─── PostgreSQL entities ──────────────────────────────────────────────────────

// User represents a registered account stored in PostgreSQL.
type User struct {
	ID        string    `db:"id"         json:"id"`
	Username  string    `db:"username"   json:"username"`
	Password  string    `db:"password"   json:"-"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Room represents a chat room stored in PostgreSQL.
type Room struct {
	ID        string    `db:"id"         json:"id"`
	Name      string    `db:"name"       json:"name"`
	OwnerID   string    `db:"owner_id"   json:"owner_id"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// RoomMember represents the many-to-many between rooms and users.
type RoomMember struct {
	RoomID   string    `db:"room_id"`
	UserID   string    `db:"user_id"`
	JoinedAt time.Time `db:"joined_at"`
}

// ─── MongoDB documents ────────────────────────────────────────────────────────

// Sender is embedded inside Message documents in MongoDB.
type Sender struct {
	ID       string `bson:"id"       json:"id"`
	Username string `bson:"username" json:"username"`
}

// Message is stored in MongoDB.
type Message struct {
	MessageID string    `bson:"message_id" json:"id"`
	RoomID    string    `bson:"room_id"    json:"room_id"`
	Sender    Sender    `bson:"sender"     json:"sender"`
	Content   string    `bson:"content"    json:"content"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	Delivered bool      `bson:"delivered"  json:"delivered"`
}

// ─── Kafka serialization ──────────────────────────────────────────────────────

// KafkaMessage is the payload published to / consumed from Kafka.
type KafkaMessage struct {
	MessageID      string    `json:"message_id"`
	RoomID         string    `json:"room_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// ─── HTTP request / response DTOs ────────────────────────────────────────────

// RegisterRequest is the payload for POST /api/v1/auth/register.
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginRequest is the payload for POST /api/v1/auth/login.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// CreateRoomRequest is the payload for POST /api/v1/rooms.
type CreateRoomRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

// AuthResponse is returned after successful register/login.
type AuthResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// RoomResponse is returned in room list / create endpoints.
type RoomResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// MessageResponse is returned in message history endpoints.
type MessageResponse struct {
	ID             string    `json:"id"`
	RoomID         string    `json:"room_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}
