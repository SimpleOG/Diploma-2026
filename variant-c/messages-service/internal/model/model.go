package model

import "time"

// Sender holds the sender's identity embedded in a message.
type Sender struct {
	ID       string `json:"id" bson:"id"`
	Username string `json:"username" bson:"username"`
}

// Message represents a chat message stored in MongoDB.
type Message struct {
	MessageID string    `json:"message_id" bson:"message_id"`
	RoomID    string    `json:"room_id" bson:"room_id"`
	Sender    Sender    `json:"sender" bson:"sender"`
	Content   string    `json:"content" bson:"content"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

// SendMessageRequest is the payload for sending a message.
type SendMessageRequest struct {
	RoomID  string `json:"room_id" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// MessageEvent is the event published to RabbitMQ.
type MessageEvent struct {
	Event          string    `json:"event"`
	MessageID      string    `json:"message_id"`
	RoomID         string    `json:"room_id"`
	SenderID       string    `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}
