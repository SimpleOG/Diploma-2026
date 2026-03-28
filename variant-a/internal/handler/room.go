package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/chat-diploma/variant-a/internal/middleware"
	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/chat-diploma/variant-a/internal/repository"
	"github.com/gin-gonic/gin"
)

// roomRepo is the subset of RoomRepository used by RoomHandler.
type roomRepo interface {
	Create(ctx context.Context, name, ownerID string) (*model.Room, error)
	List(ctx context.Context) ([]model.RoomResponse, error)
	GetByID(ctx context.Context, id string) (*model.Room, error)
	AddMember(ctx context.Context, roomID, userID string) error
	IsMember(ctx context.Context, roomID, userID string) (bool, error)
}

// msgRepo is the subset of MessageRepository used by RoomHandler.
type msgRepo interface {
	ListByRoom(ctx context.Context, roomID string, before string, limit int) ([]model.Message, bool, error)
}

// RoomRepoForTest is the exported version of roomRepo for use in external test packages.
type RoomRepoForTest = roomRepo

// MsgRepoForTest is the exported version of msgRepo for use in external test packages.
type MsgRepoForTest = msgRepo

// RoomHandler handles room-related HTTP endpoints.
type RoomHandler struct {
	roomRepo roomRepo
	msgRepo  msgRepo
}

// NewRoomHandler constructs a RoomHandler.
func NewRoomHandler(roomRepo roomRepo, msgRepo msgRepo) *RoomHandler {
	return &RoomHandler{
		roomRepo: roomRepo,
		msgRepo:  msgRepo,
	}
}

// ListRooms handles GET /api/v1/rooms.
// Returns all rooms with member counts.
func (h *RoomHandler) ListRooms(c *gin.Context) {
	rooms, err := h.roomRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rooms"})
		return
	}

	if rooms == nil {
		rooms = []model.RoomResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"rooms": rooms})
}

// CreateRoom handles POST /api/v1/rooms.
// Creates a room and automatically adds the creator as a member.
func (h *RoomHandler) CreateRoom(c *gin.Context) {
	var req model.CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ownerID, _ := c.Get(middleware.ContextUserID)
	uid, _ := ownerID.(string)

	ctx := c.Request.Context()

	room, err := h.roomRepo.Create(ctx, req.Name, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create room"})
		return
	}

	// Auto-add owner as member.
	if err := h.roomRepo.AddMember(ctx, room.ID, uid); err != nil && !errors.Is(err, repository.ErrDuplicate) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add owner as member"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"room": model.RoomResponse{
		ID:          room.ID,
		Name:        room.Name,
		OwnerID:     room.OwnerID,
		CreatedAt:   room.CreatedAt,
		MemberCount: 1,
	}})
}

// JoinRoom handles POST /api/v1/rooms/:id/join.
// Adds the authenticated user to the specified room.
func (h *RoomHandler) JoinRoom(c *gin.Context) {
	roomID := c.Param("id")
	userID, _ := c.Get(middleware.ContextUserID)
	uid, _ := userID.(string)

	ctx := c.Request.Context()

	// Verify the room exists.
	if _, err := h.roomRepo.GetByID(ctx, roomID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if err := h.roomRepo.AddMember(ctx, roomID, uid); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			c.JSON(http.StatusConflict, gin.H{"error": "already a member"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to join room"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "joined room"})
}

// GetMessages handles GET /api/v1/rooms/:id/messages.
// Supports cursor-based pagination via `before` (message ID) and `limit` query params.
func (h *RoomHandler) GetMessages(c *gin.Context) {
	roomID := c.Param("id")
	userID, _ := c.Get(middleware.ContextUserID)
	uid, _ := userID.(string)

	ctx := c.Request.Context()

	// Verify the room exists.
	if _, err := h.roomRepo.GetByID(ctx, roomID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Verify the user is a member.
	isMember, err := h.roomRepo.IsMember(ctx, roomID, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}

	before := c.Query("before")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	messages, hasMore, err := h.msgRepo.ListByRoom(ctx, roomID, before, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch messages"})
		return
	}

	if messages == nil {
		messages = []model.Message{}
	}

	resp := model.MessageResponse{
		Messages: messages,
		HasMore:  hasMore,
	}
	if hasMore && len(messages) > 0 {
		resp.NextCursor = messages[len(messages)-1].ID
	}

	c.JSON(http.StatusOK, resp)
}
