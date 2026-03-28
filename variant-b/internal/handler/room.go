package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/chat-diploma/variant-b/internal/middleware"
	"github.com/chat-diploma/variant-b/internal/model"
	"github.com/chat-diploma/variant-b/internal/repository"
	"github.com/gin-gonic/gin"
)

// RoomHandler handles room and message history endpoints.
type RoomHandler struct {
	roomRepo    *repository.RoomRepository
	messageRepo *repository.MessageRepository
}

// NewRoomHandler creates a new RoomHandler.
func NewRoomHandler(roomRepo *repository.RoomRepository, messageRepo *repository.MessageRepository) *RoomHandler {
	return &RoomHandler{
		roomRepo:    roomRepo,
		messageRepo: messageRepo,
	}
}

// Create handles POST /api/v1/rooms.
func (h *RoomHandler) Create(c *gin.Context) {
	var req model.CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ownerID := c.GetString(middleware.ContextKeyUserID)
	room, err := h.roomRepo.Create(c.Request.Context(), req.Name, ownerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create room"})
		return
	}

	c.JSON(http.StatusCreated, model.RoomResponse{
		ID:        room.ID,
		Name:      room.Name,
		OwnerID:   room.OwnerID,
		CreatedAt: room.CreatedAt,
	})
}

// List handles GET /api/v1/rooms.
func (h *RoomHandler) List(c *gin.Context) {
	rooms, err := h.roomRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rooms"})
		return
	}

	resp := make([]model.RoomResponse, 0, len(rooms))
	for _, r := range rooms {
		resp = append(resp, model.RoomResponse{
			ID:        r.ID,
			Name:      r.Name,
			OwnerID:   r.OwnerID,
			CreatedAt: r.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, resp)
}

// Join handles POST /api/v1/rooms/:id/join.
func (h *RoomHandler) Join(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString(middleware.ContextKeyUserID)

	if _, err := h.roomRepo.GetByID(c.Request.Context(), roomID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if err := h.roomRepo.AddMember(c.Request.Context(), roomID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join room"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "joined room"})
}

// Messages handles GET /api/v1/rooms/:id/messages – paginated message history from MongoDB.
func (h *RoomHandler) Messages(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString(middleware.ContextKeyUserID)

	// Verify membership.
	ok, err := h.roomRepo.IsMember(c.Request.Context(), roomID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}

	before := c.Query("before") // optional cursor (RFC3339Nano)
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 50
	}

	msgs, hasMore, err := h.messageRepo.ListByRoom(c.Request.Context(), roomID, before, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch messages"})
		return
	}

	resp := make([]model.MessageResponse, 0, len(msgs))
	for _, m := range msgs {
		resp = append(resp, model.MessageResponse{
			ID:             m.MessageID,
			RoomID:         m.RoomID,
			SenderID:       m.Sender.ID,
			SenderUsername: m.Sender.Username,
			Content:        m.Content,
			CreatedAt:      m.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": resp,
		"has_more": hasMore,
	})
}
