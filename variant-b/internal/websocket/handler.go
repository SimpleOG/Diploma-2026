package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chat-diploma/variant-b/internal/cache"
	"github.com/chat-diploma/variant-b/internal/kafka"
	"github.com/chat-diploma/variant-b/internal/middleware"
	"github.com/chat-diploma/variant-b/internal/model"
	"github.com/chat-diploma/variant-b/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// incomingMessage is the structure sent by WebSocket clients.
type incomingMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// outgoingMessage is the structure broadcast to all room members.
type outgoingMessage struct {
	Type      string    `json:"type"`
	MessageID string    `json:"message_id"`
	RoomID    string    `json:"room_id"`
	SenderID  string    `json:"sender_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// ackMessage is sent back to the sender after enqueuing.
type ackMessage struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id"`
}

// Handler holds dependencies for the WebSocket upgrade and message processing.
type Handler struct {
	hub        *Hub
	roomRepo   *repository.RoomRepository
	memcached  *cache.MemcachedClient
	producer   *kafka.KafkaProducer
	kafkaTopic string
}

// NewHandler creates a new WebSocket Handler.
func NewHandler(
	hub *Hub,
	roomRepo *repository.RoomRepository,
	memcached *cache.MemcachedClient,
	producer *kafka.KafkaProducer,
	kafkaTopic string,
) *Handler {
	return &Handler{
		hub:        hub,
		roomRepo:   roomRepo,
		memcached:  memcached,
		producer:   producer,
		kafkaTopic: kafkaTopic,
	}
}

// ServeWS handles an incoming WebSocket upgrade request.
// Query parameter: room_id (required).
func (h *Handler) ServeWS(c *gin.Context) {
	roomID := c.Query("room_id")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "room_id query parameter is required"})
		return
	}

	userID := c.GetString(middleware.ContextKeyUserID)
	username := c.GetString(middleware.ContextKeyUsername)

	// Verify the room exists.
	if _, err := h.roomRepo.GetByID(c.Request.Context(), roomID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("ws: upgrade", "error", err)
		return
	}

	client := NewClient(h.hub, conn, roomID, userID, username, h)
	go client.WritePump()
	go client.ReadPump()
}

// HandleIncoming processes a raw message received from a client.
func (h *Handler) HandleIncoming(c *Client, raw []byte) {
	var msg incomingMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		slog.Warn("ws: invalid message format", "user", c.userID, "error", err)
		return
	}

	if msg.Type != "message" {
		return
	}

	content := msg.Content
	if len(content) == 0 || len(content) > 4000 {
		slog.Warn("ws: message content out of bounds", "user", c.userID, "len", len(content))
		return
	}

	// Check membership: Memcached first, then PostgreSQL on miss.
	isMember, err := h.checkMembership(c.roomID, c.userID)
	if err != nil {
		slog.Error("ws: membership check error", "user", c.userID, "room", c.roomID, "error", err)
		return
	}
	if !isMember {
		slog.Warn("ws: non-member attempted to send message", "user", c.userID, "room", c.roomID)
		return
	}

	messageID := uuid.New().String()
	createdAt := time.Now().UTC()

	// Publish to Kafka (async, fire-and-forget).
	km := model.KafkaMessage{
		MessageID:      messageID,
		RoomID:         c.roomID,
		SenderID:       c.userID,
		SenderUsername: c.username,
		Content:        content,
		CreatedAt:      createdAt,
	}
	payload, err := json.Marshal(km)
	if err != nil {
		slog.Error("ws: marshal kafka message", "error", err)
		return
	}
	if err := h.producer.Publish(h.kafkaTopic, c.roomID, payload); err != nil {
		slog.Error("ws: publish to kafka", "error", err)
		// Continue – optimistic delivery still happens.
	}

	// Optimistic broadcast to all room members via Hub.
	outgoing := outgoingMessage{
		Type:      "message",
		MessageID: messageID,
		RoomID:    c.roomID,
		SenderID:  c.userID,
		Username:  c.username,
		Content:   content,
		CreatedAt: createdAt,
	}
	broadcastData, err := json.Marshal(outgoing)
	if err != nil {
		slog.Error("ws: marshal broadcast message", "error", err)
		return
	}
	h.hub.Broadcast(c.roomID, broadcastData)

	// ACK back to the sender.
	c.SendJSON(ackMessage{Type: "ack", MessageID: messageID})
}

// contextWithTimeout returns a 5-second background context.
func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// checkMembership consults Memcached first; on miss it queries PostgreSQL and
// populates the cache.
func (h *Handler) checkMembership(roomID, userID string) (bool, error) {
	cached, err := h.memcached.IsMember(roomID, userID)
	if err == nil && cached {
		return true, nil
	}
	if err != nil {
		slog.Warn("ws: memcached IsMember error, falling back to DB", "error", err)
	}

	// Fall back to PostgreSQL.
	ctx, cancel := contextWithTimeout()
	defer cancel()

	ok, dbErr := h.roomRepo.IsMember(ctx, roomID, userID)
	if dbErr != nil {
		return false, fmt.Errorf("ws: db membership check: %w", dbErr)
	}

	if ok {
		if cacheErr := h.memcached.SetMember(roomID, userID); cacheErr != nil {
			slog.Warn("ws: memcached SetMember error", "error", cacheErr)
		}
	}
	return ok, nil
}
