package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/chat-diploma/variant-c/messages-service/internal/model"
	"github.com/chat-diploma/variant-c/messages-service/internal/rabbitmq"
	"github.com/chat-diploma/variant-c/messages-service/internal/repository"
)

// MessageHandler handles message-related HTTP requests.
type MessageHandler struct {
	msgRepo         *repository.MessageRepository
	publisher       *rabbitmq.Publisher
	roomsServiceURL string
	httpClient      *http.Client
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(
	msgRepo *repository.MessageRepository,
	publisher *rabbitmq.Publisher,
	roomsServiceURL string,
) *MessageHandler {
	return &MessageHandler{
		msgRepo:         msgRepo,
		publisher:       publisher,
		roomsServiceURL: roomsServiceURL,
		httpClient:      &http.Client{Timeout: 5 * time.Second},
	}
}

// SendMessage handles POST /api/v1/messages.
func (h *MessageHandler) SendMessage(c *gin.Context) {
	var req model.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate content.
	if len(req.Content) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content cannot be empty"})
		return
	}
	if len(req.Content) > 4096 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content exceeds maximum length of 4096 characters"})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user identity"})
		return
	}

	username, _ := c.Get("username")
	usernameStr, _ := username.(string)

	// Check room membership via rooms-service.
	isMember, err := h.checkMembership(c.Request.Context(), req.RoomID, userIDStr)
	if err != nil {
		slog.Error("failed to check room membership", "room_id", req.RoomID, "user_id", userIDStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify room membership"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}

	// Build and save message.
	msg := &model.Message{
		MessageID: uuid.New().String(),
		RoomID:    req.RoomID,
		Sender: model.Sender{
			ID:       userIDStr,
			Username: usernameStr,
		},
		Content:   req.Content,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.msgRepo.Insert(c.Request.Context(), msg); err != nil {
		slog.Error("failed to insert message", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Publish to RabbitMQ.
	event := model.MessageEvent{
		Event:          "new_message",
		MessageID:      msg.MessageID,
		RoomID:         msg.RoomID,
		SenderID:       msg.Sender.ID,
		SenderUsername: msg.Sender.Username,
		Content:        msg.Content,
		CreatedAt:      msg.CreatedAt,
	}

	eventBody, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal event", "error", err)
		// Don't fail the request - message was saved, just log the error.
	} else {
		routingKey := fmt.Sprintf("room.%s.message", msg.RoomID)
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		if err := h.publisher.Publish(pubCtx, routingKey, eventBody); err != nil {
			slog.Error("failed to publish message event", "error", err)
			// Don't fail - message is already persisted.
		}
	}

	c.JSON(http.StatusCreated, msg)
}

// ListMessages handles GET /api/v1/rooms/:room_id/messages.
func (h *MessageHandler) ListMessages(c *gin.Context) {
	roomID := c.Param("room_id")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "room_id is required"})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user identity"})
		return
	}

	// Check room membership.
	isMember, err := h.checkMembership(c.Request.Context(), roomID, userIDStr)
	if err != nil {
		slog.Error("failed to check room membership", "room_id", roomID, "user_id", userIDStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify room membership"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}

	// Parse limit.
	var limit int64 = 50
	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.ParseInt(limitStr, 10, 64)
		if err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// Parse cursor (before timestamp).
	var beforeTime time.Time
	if beforeStr := c.Query("before"); beforeStr != "" {
		parsed, err := time.Parse(time.RFC3339Nano, beforeStr)
		if err == nil {
			beforeTime = parsed
		}
	}

	messages, err := h.msgRepo.ListByRoom(c.Request.Context(), roomID, beforeTime, limit)
	if err != nil {
		slog.Error("failed to list messages", "room_id", roomID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// checkMembership calls rooms-service to verify room membership with retry logic.
func (h *MessageHandler) checkMembership(ctx context.Context, roomID, userID string) (bool, error) {
	url := fmt.Sprintf("%s/internal/rooms/%s/members/%s", h.roomsServiceURL, roomID, userID)
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		status, err := h.doMembershipCheck(ctx, url)
		if err == nil {
			return status == http.StatusOK, nil
		}
		lastErr = err

		if attempt < len(backoffs) {
			select {
			case <-time.After(backoffs[attempt]):
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
	}
	return false, fmt.Errorf("membership check failed after retries: %w", lastErr)
}

// doMembershipCheck performs a single HTTP request to rooms-service.
func (h *MessageHandler) doMembershipCheck(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}
