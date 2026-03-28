package model

import "time"

// User represents a registered user in the system.
type User struct {
	ID        string    `json:"id" db:"id"`
	Username  string    `json:"username" db:"username"`
	Password  string    `json:"-" db:"password"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// RegisterRequest is the payload for user registration.
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

// LoginRequest is the payload for user login.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse is returned after successful register or login.
type AuthResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// ValidateTokenRequest is the payload for internal token validation.
type ValidateTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// ValidateTokenResponse is returned by the internal validate endpoint.
type ValidateTokenResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}
