package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/chat-diploma/variant-c/auth-service/internal/model"
	"github.com/chat-diploma/variant-c/auth-service/internal/repository"
)

// AuthHandler handles authentication-related HTTP requests.
type AuthHandler struct {
	userRepo         *repository.UserRepository
	jwtSecret        []byte
	jwtExpirationHrs int
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(
	userRepo *repository.UserRepository,
	jwtSecret string,
	jwtExpirationHrs int,
) *AuthHandler {
	return &AuthHandler{
		userRepo:         userRepo,
		jwtSecret:        []byte(jwtSecret),
		jwtExpirationHrs: jwtExpirationHrs,
	}
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	user, err := h.userRepo.Create(req.Username, string(hashed))
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateUsername) {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		slog.Error("failed to create user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	token, err := h.generateToken(user.ID, user.Username)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
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

	user, err := h.userRepo.GetByUsername(req.Username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		slog.Error("failed to get user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := h.generateToken(user.ID, user.Username)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, model.AuthResponse{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
	})
}

// ValidateToken handles POST /internal/auth/validate.
// This endpoint does NOT require JWT middleware.
func (h *AuthHandler) ValidateToken(c *gin.Context) {
	var req model.ValidateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	claims, err := h.parseToken(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
		return
	}

	username, _ := claims["username"].(string)

	c.JSON(http.StatusOK, model.ValidateTokenResponse{
		UserID:   userID,
		Username: username,
	})
}

// generateToken creates a signed JWT for the given user.
func (h *AuthHandler) generateToken(userID, username string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":      userID,
		"username": username,
		"iat":      now.Unix(),
		"exp":      now.Add(time.Duration(h.jwtExpirationHrs) * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}

// parseToken validates and parses a JWT string.
func (h *AuthHandler) parseToken(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return h.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
