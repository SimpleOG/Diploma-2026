package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chat-diploma/variant-a/internal/middleware"
	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/chat-diploma/variant-a/internal/repository"
	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins in development. In production this should validate
		// the Origin header against a configured allowlist.
		return true
	},
}

// messageRepo is the interface MessageHandler needs from the message repository.
type messageRepo interface {
	Create(ctx context.Context, roomID, senderID, content string) (*model.Message, error)
}

// roomRepo is the interface MessageHandler needs from the room repository.
type roomRepo interface {
	IsMember(ctx context.Context, roomID, userID string) (bool, error)
}

// MessageHandler handles incoming WebSocket chat messages.
type MessageHandler struct {
	msgRepo     messageRepo
	roomRepo    roomRepo
	hub         *Hub
	redisClient *redis.Client
}

// NewMessageHandler constructs a MessageHandler.
func NewMessageHandler(
	msgRepo messageRepo,
	roomRepo roomRepo,
	hub *Hub,
	redisClient *redis.Client,
) *MessageHandler {
	return &MessageHandler{
		msgRepo:     msgRepo,
		roomRepo:    roomRepo,
		hub:         hub,
		redisClient: redisClient,
	}
}

// HandleIncoming validates, persists, and broadcasts a chat message.
func (h *MessageHandler) HandleIncoming(client *Client, roomID, content string) error {
	if content == "" {
		return errors.New("content is empty")
	}
	if len([]rune(content)) > 4096 {
		return errors.New("content exceeds 4096 characters")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check membership: try Redis first, fall back to DB.
	isMember, err := h.checkMembership(ctx, roomID, client.userID)
	if err != nil {
		return fmt.Errorf("membership check: %w", err)
	}
	if !isMember {
		return errors.New("user is not a member of the room")
	}

	// Persist to PostgreSQL.
	msg, err := h.msgRepo.Create(ctx, roomID, client.userID, content)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}

	// Build JSON payload.
	payload, err := json.Marshal(map[string]interface{}{
		"type":            "message",
		"id":              msg.ID,
		"room_id":         msg.RoomID,
		"sender_id":       msg.SenderID,
		"sender_username": msg.SenderUsername,
		"content":         msg.Content,
		"created_at":      msg.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Broadcast to local hub (which also publishes to Redis).
	h.hub.Broadcast(&BroadcastMessage{
		RoomID:  roomID,
		Payload: payload,
		Exclude: nil, // sender also receives the confirmed message
	})

	return nil
}

// checkMembership returns true if userID is a member of roomID.
// It checks Redis first (SISMEMBER) and falls back to the DB.
func (h *MessageHandler) checkMembership(ctx context.Context, roomID, userID string) (bool, error) {
	if h.redisClient != nil {
		key := fmt.Sprintf("room:members:%s", roomID)
		isMember, err := h.redisClient.SIsMember(ctx, key, userID).Result()
		if err == nil {
			if isMember {
				return true, nil
			}
			// Not in Redis cache – fall through to DB check.
		} else {
			slog.Warn("redis SISMEMBER failed, falling back to DB", "err", err)
		}
	}

	return h.roomRepo.IsMember(ctx, roomID, userID)
}

// ServeWS upgrades an HTTP connection to WebSocket and launches the client pumps.
func ServeWS(hub *Hub, msgHandler MessageHandlerInterface) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get(middleware.ContextUserID)
		username, _ := c.Get(middleware.ContextUsername)

		uid, ok := userID.(string)
		if !ok || uid == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		uname, _ := username.(string)

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Error("ws upgrade failed", "err", err)
			return
		}

		client := NewClient(conn, uid, uname, hub)

		go client.writePump()
		go client.readPump(msgHandler)
	}
}

// CacheMembership stores roomID/userID in Redis for fast membership checks.
func CacheMembership(ctx context.Context, redisClient *redis.Client, roomID, userID string) error {
	if redisClient == nil {
		return nil
	}
	key := fmt.Sprintf("room:members:%s", roomID)
	return redisClient.SAdd(ctx, key, userID).Err()
}

// InvalidateMembershipCache removes the membership cache for a room.
func InvalidateMembershipCache(ctx context.Context, redisClient *redis.Client, roomID string) error {
	if redisClient == nil {
		return nil
	}
	key := fmt.Sprintf("room:members:%s", roomID)
	return redisClient.Del(ctx, key).Err()
}

// Ensure MessageHandler implements MessageHandlerInterface.
var _ MessageHandlerInterface = (*MessageHandler)(nil)

// roomMembersKey returns the Redis key for a room's member set.
func roomMembersKey(roomID string) string {
	return fmt.Sprintf("room:members:%s", roomID)
}

// PopulateMembershipCache loads all members of a room from DB into Redis.
func PopulateMembershipCache(
	ctx context.Context,
	redisClient *redis.Client,
	roomID string,
	members []string,
) error {
	if redisClient == nil || len(members) == 0 {
		return nil
	}
	key := roomMembersKey(roomID)
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	return redisClient.SAdd(ctx, key, args...).Err()
}
