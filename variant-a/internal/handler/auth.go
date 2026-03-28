package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/chat-diploma/variant-a/internal/auth"
	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/chat-diploma/variant-a/internal/repository"
	"github.com/gin-gonic/gin"
)

// userCreator is the subset of UserRepository used by AuthHandler.
type userCreator interface {
	Create(ctx context.Context, username, hashedPassword string) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
}

// UserRepoForTest is the exported version of userCreator for use in external test packages.
type UserRepoForTest = userCreator

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	userRepo userCreator
	authSvc  *auth.Service
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(userRepo userCreator, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{
		userRepo: userRepo,
		authSvc:  authSvc,
	}
}

// Register handles POST /api/v1/auth/register.
// It validates the request, hashes the password, creates the user, and returns a JWT.
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashed, err := h.authSvc.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process password"})
		return
	}

	user, err := h.userRepo.Create(c.Request.Context(), req.Username, hashed)
	if err != nil {
		// Duplicate username – PostgreSQL unique_violation (23505) is wrapped in
		// the repository layer.
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
// It validates credentials and returns a JWT on success.
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if !h.authSvc.CheckPassword(user.Password, req.Password) {
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
