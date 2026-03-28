package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-b/internal/model"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("repository: record not found")

// UserRepository handles PostgreSQL persistence for users.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user row and returns the auto-generated UUID.
func (r *UserRepository) Create(ctx context.Context, username, hashedPassword string) (*model.User, error) {
	const q = `
		INSERT INTO users (username, password)
		VALUES ($1, $2)
		RETURNING id, username, password, created_at`

	row := r.db.QueryRowContext(ctx, q, username, hashedPassword)
	var u model.User
	if err := row.Scan(&u.ID, &u.Username, &u.Password, &u.CreatedAt); err != nil {
		return nil, fmt.Errorf("repository: create user: %w", err)
	}
	return &u, nil
}

// GetByUsername fetches a user by username, returning ErrNotFound when absent.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	const q = `SELECT id, username, password, created_at FROM users WHERE username = $1`

	row := r.db.QueryRowContext(ctx, q, username)
	var u model.User
	if err := row.Scan(&u.ID, &u.Username, &u.Password, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: get user by username: %w", err)
	}
	return &u, nil
}

// GetByID fetches a user by primary key, returning ErrNotFound when absent.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	const q = `SELECT id, username, password, created_at FROM users WHERE id = $1`

	row := r.db.QueryRowContext(ctx, q, id)
	var u model.User
	if err := row.Scan(&u.ID, &u.Username, &u.Password, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: get user by id: %w", err)
	}
	return &u, nil
}
