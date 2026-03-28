package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chat-diploma/variant-a/internal/model"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("not found")

// UserRepository handles persistence for users.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user and returns the created record.
func (r *UserRepository) Create(ctx context.Context, username, hashedPassword string) (*model.User, error) {
	const query = `
		INSERT INTO users (username, password)
		VALUES ($1, $2)
		RETURNING id, created_at`

	user := &model.User{
		Username: username,
		Password: hashedPassword,
	}

	err := r.db.QueryRowContext(ctx, query, username, hashedPassword).Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("UserRepository.Create: %w", err)
	}

	return user, nil
}

// GetByUsername looks up a user by their username.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	const query = `
		SELECT id, username, password, created_at
		FROM users
		WHERE username = $1`

	user := &model.User{}
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.GetByUsername: %w", err)
	}

	return user, nil
}

// GetByID looks up a user by their UUID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	const query = `
		SELECT id, username, password, created_at
		FROM users
		WHERE id = $1`

	user := &model.User{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Password, &user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("UserRepository.GetByID: %w", err)
	}

	return user, nil
}
