package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// ErrInvalidToken is returned when a token cannot be parsed or validated.
var ErrInvalidToken = errors.New("invalid token")

// Service provides JWT and password utilities.
type Service struct {
	secret     []byte
	expiration time.Duration
}

// NewService creates a new auth Service.
func NewService(secret string, expirationHours int) *Service {
	return &Service{
		secret:     []byte(secret),
		expiration: time.Duration(expirationHours) * time.Hour,
	}
}

// claims is the JWT payload.
type claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed HS256 JWT for the given user.
func (s *Service) GenerateToken(userID, username string) (string, error) {
	now := time.Now()
	c := &claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiration)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("auth.GenerateToken: %w", err)
	}

	return signed, nil
}

// ValidateToken parses and validates a JWT string.
// Returns userID (sub) and username on success.
func (s *Service) ValidateToken(tokenStr string) (userID, username string, err error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return "", "", ErrInvalidToken
	}

	return c.Subject, c.Username, nil
}

// HashPassword hashes a plaintext password with bcrypt (cost 12).
func (s *Service) HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth.HashPassword: %w", err)
	}
	return string(hashed), nil
}

// CheckPassword compares a bcrypt hash with a plaintext password.
func (s *Service) CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
