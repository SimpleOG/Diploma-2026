package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/chat-diploma/variant-c/rooms-service/internal/model"
	"github.com/chat-diploma/variant-c/rooms-service/internal/repository"
)

// RoomHandler handles room-related HTTP requests.
type RoomHandler struct {
	roomRepo    *repository.RoomRepository
	redisClient *redis.Client
}

// NewRoomHandler creates a new RoomHandler.
func NewRoomHandler(roomRepo *repository.RoomRepository, redisClient *redis.Client) *RoomHandler {
	return &RoomHandler{
		roomRepo:    roomRepo,
		redisClient: redisClient,
	}
}

// ListRooms handles GET /api/v1/rooms.
func (h *RoomHandler) ListRooms(c *gin.Context) {
	rooms, err := h.roomRepo.List()
	if err != nil {
		slog.Error("failed to list rooms", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, rooms)
}

// CreateRoom handles POST /api/v1/rooms.
func (h *RoomHandler) CreateRoom(c *gin.Context) {
	var req model.CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ownerID, _ := c.Get("user_id")
	ownerIDStr, ok := ownerID.(string)
	if !ok || ownerIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user identity"})
		return
	}

	room, err := h.roomRepo.Create(req.Name, ownerIDStr)
	if err != nil {
		slog.Error("failed to create room", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	resp := model.RoomResponse{
		ID:          room.ID,
		Name:        room.Name,
		OwnerID:     room.OwnerID,
		MemberCount: 1, // owner is added automatically
		CreatedAt:   room.CreatedAt,
	}

	c.JSON(http.StatusCreated, resp)
}

// JoinRoom handles POST /api/v1/rooms/:room_id/join.
func (h *RoomHandler) JoinRoom(c *gin.Context) {
	roomID := c.Param("room_id")

	userID, _ := c.Get("user_id")
	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user identity"})
		return
	}

	// Verify room exists.
	_, err := h.roomRepo.GetByID(roomID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
			return
		}
		slog.Error("failed to get room", "room_id", roomID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if err := h.roomRepo.AddMember(roomID, userIDStr); err != nil {
		if errors.Is(err, repository.ErrAlreadyMember) {
			c.JSON(http.StatusConflict, gin.H{"error": "already a member of this room"})
			return
		}
		slog.Error("failed to add member", "room_id", roomID, "user_id", userIDStr, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Invalidate membership cache.
	if h.redisClient != nil {
		cacheKey := memberCacheKey(roomID, userIDStr)
		_ = h.redisClient.Del(context.Background(), cacheKey).Err()
	}

	c.JSON(http.StatusOK, model.JoinResponse{
		RoomID:  roomID,
		UserID:  userIDStr,
		Message: "joined successfully",
	})
}

// CheckMembership handles GET /internal/rooms/:room_id/members/:user_id.
// No auth middleware - called by other services.
func (h *RoomHandler) CheckMembership(c *gin.Context) {
	roomID := c.Param("room_id")
	userID := c.Param("user_id")

	ctx := c.Request.Context()

	// Check membership cache.
	if h.redisClient != nil {
		cacheKey := memberCacheKey(roomID, userID)
		val, err := h.redisClient.Get(ctx, cacheKey).Result()
		if err == nil {
			if val == "1" {
				c.JSON(http.StatusOK, gin.H{"member": true})
			} else {
				c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
			}
			return
		}
	}

	// Verify room exists.
	_, err := h.roomRepo.GetByID(roomID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room not found"})
			return
		}
		slog.Error("failed to get room", "room_id", roomID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	isMember, err := h.roomRepo.IsMember(roomID, userID)
	if err != nil {
		slog.Error("failed to check membership", "room_id", roomID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Cache the result.
	if h.redisClient != nil {
		cacheKey := memberCacheKey(roomID, userID)
		cacheVal := "0"
		if isMember {
			cacheVal = "1"
		}
		_ = h.redisClient.Set(ctx, cacheKey, cacheVal, 300*time.Second).Err()
	}

	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"member": true})
}

// memberCacheKey builds the Redis cache key for membership checks.
func memberCacheKey(roomID, userID string) string {
	return fmt.Sprintf("member:%s:%s", roomID, userID)
}
