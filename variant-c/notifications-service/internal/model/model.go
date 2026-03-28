package model

import "time"

// MessageEvent is the event consumed from RabbitMQ.
type MessageEvent struct {
	Event          string    `json:"event"`
	MessageID      string    `json:"message_id"`
	RoomID         string    `json:"room_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// WSMessage is the base envelope for all WebSocket messages.
type WSMessage struct {
	Type string `json:"type"`
}

// WSIncoming is a client-to-server WebSocket message.
type WSIncoming struct {
	Type   string `json:"type"`   // "join", "leave", "ping"
	RoomID string `json:"room_id"` // used by join/leave
}

// WSOutgoing is a server-to-client WebSocket message.
type WSOutgoing struct {
	Type           string    `json:"type"`
	MessageID      string    `json:"message_id,omitempty"`
	RoomID         string    `json:"room_id,omitempty"`
	SenderID       string    `json:"sender_id,omitempty"`
	SenderUsername string    `json:"sender_username,omitempty"`
	Content        string    `json:"content,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}
