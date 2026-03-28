package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-c/auth-service/internal/model"
)

// ErrNotFound is returned when a user is not found.
var ErrNotFound = errors.New("user not found")

// ErrDuplicateUsername is returned when a username already exists.
var ErrDuplicateUsername = errors.New("username already exists")

// UserRepository handles user persistence.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user into the database and returns the created user.
func (r *UserRepository) Create(username, hashedPassword string) (*model.User, error) {
	const q = `
		INSERT INTO users (username, password)
		VALUES ($1, $2)
		RETURNING id, username, password, created_at
	`
	user := &model.User{}
	err := r.db.QueryRow(q, username, hashedPassword).Scan(
		&user.ID, &user.Username, &user.Password, &user.CreatedAt,
	)
	if err != nil {
		// Check for unique constraint violation (PostgreSQL error code 23505).
		if isDuplicateError(err) {
			return nil, ErrDuplicateUsername
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// GetByUsername retrieves a user by their username.
func (r *UserRepository) GetByUsername(username string) (*model.User, error) {
	const q = `SELECT id, username, password, created_at FROM users WHERE username = $1`
	user := &model.User{}
	err := r.db.QueryRow(q, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return user, nil
}

// GetByID retrieves a user by their ID.
func (r *UserRepository) GetByID(id string) (*model.User, error) {
	const q = `SELECT id, username, password, created_at FROM users WHERE id = $1`
	user := &model.User{}
	err := r.db.QueryRow(q, id).Scan(
		&user.ID, &user.Username, &user.Password, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

// isDuplicateError checks if the error is a PostgreSQL unique constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "23505") || contains(err.Error(), "duplicate key")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
