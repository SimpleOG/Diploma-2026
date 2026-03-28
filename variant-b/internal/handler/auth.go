package handler

import (
	"errors"
	"net/http"

	"github.com/chat-diploma/variant-b/internal/auth"
	"github.com/chat-diploma/variant-b/internal/model"
	"github.com/chat-diploma/variant-b/internal/repository"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	userRepo *repository.UserRepository
	authSvc  *auth.Service
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(userRepo *repository.UserRepository, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, authSvc: authSvc}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashed, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process password"})
		return
	}

	user, err := h.userRepo.Create(c.Request.Context(), req.Username, hashed)
	if err != nil {
		// Detect unique constraint violation (PostgreSQL error code 23505).
		c.JSON(http.StatusConflict, gin.H{"error": "username already taken"})
		return
	}

	token, err := h.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, model.AuthResponse{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
	})
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userRepo.GetByUsername(c.Request.Context(), req.Username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if err := auth.CheckPassword(user.Password, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := h.authSvc.GenerateToken(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, model.AuthResponse{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
	})
}
