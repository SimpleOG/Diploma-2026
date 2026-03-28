package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chat-diploma/variant-c/auth-service/internal/handler"
	"github.com/chat-diploma/variant-c/auth-service/internal/model"
	"github.com/chat-diploma/variant-c/auth-service/internal/repository"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// inMemoryUserStore is a simple in-memory store for testing.
type inMemoryUserStore struct {
	users map[string]*model.User
}

// mockDB provides a *sql.DB substitute.
// Since repository.UserRepository requires a real *sql.DB, we test via
// a thin functional approach using a real SQLite or by abstracting behind an interface.
// For these tests we use a repository interface.

// UserRepository interface for testability.
type userRepoInterface interface {
	Create(username, hashedPassword string) (*model.User, error)
	GetByUsername(username string) (*model.User, error)
	GetByID(id string) (*model.User, error)
}

// We test the HTTP layer using a minimal stub that satisfies the real handler constructor.
// Since handler.NewAuthHandler takes *repository.UserRepository (concrete), we use
// integration-style tests with a throwaway in-process SQLite is not available here.
// Instead we test via the public API shape using table-driven tests.

func setupRouter(jwtSecret string, jwtExpHrs int, db *sql.DB) *gin.Engine {
	repo := repository.NewUserRepository(db)
	h := handler.NewAuthHandler(repo, jwtSecret, jwtExpHrs)

	r := gin.New()
	r.POST("/api/v1/auth/register", h.Register)
	r.POST("/api/v1/auth/login", h.Login)
	r.POST("/internal/auth/validate", h.ValidateToken)
	return r
}

// TestRegisterValidation tests that the register endpoint rejects malformed input.
func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		wantStatusCode int
	}{
		{
			name:           "missing username",
			body:           map[string]interface{}{"password": "secret123"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "missing password",
			body:           map[string]interface{}{"username": "alice"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "username too short",
			body:           map[string]interface{}{"username": "ab", "password": "secret123"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "password too short",
			body:           map[string]interface{}{"username": "alice", "password": "12345"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "empty body",
			body:           map[string]interface{}{},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	// We only need to test validation - no DB needed.
	// Use a nil DB to verify the handler rejects before DB access.
	r := gin.New()
	h := handler.NewAuthHandler(repository.NewUserRepository(nil), "test-secret", 24)
	r.POST("/api/v1/auth/register", h.Register)
	r.POST("/api/v1/auth/login", h.Login)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("want status %d, got %d; body: %s", tt.wantStatusCode, w.Code, w.Body.String())
			}

			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("response is not valid JSON: %s", w.Body.String())
			}
			if _, ok := resp["error"]; !ok {
				t.Error("expected 'error' key in response body")
			}
		})
	}
}

// TestLoginValidation tests that the login endpoint rejects malformed input.
func TestLoginValidation(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		wantStatusCode int
	}{
		{
			name:           "missing username",
			body:           map[string]interface{}{"password": "secret123"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "missing password",
			body:           map[string]interface{}{"username": "alice"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "empty body",
			body:           map[string]interface{}{},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	r := gin.New()
	h := handler.NewAuthHandler(repository.NewUserRepository(nil), "test-secret", 24)
	r.POST("/api/v1/auth/login", h.Login)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("want status %d, got %d; body: %s", tt.wantStatusCode, w.Code, w.Body.String())
			}
		})
	}
}

// TestValidateTokenInvalid tests that the validate endpoint rejects invalid tokens.
func TestValidateTokenInvalid(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		wantStatusCode int
	}{
		{
			name:           "missing token",
			body:           map[string]interface{}{},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "invalid token",
			body:           map[string]interface{}{"token": "this.is.invalid"},
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:           "empty token",
			body:           map[string]interface{}{"token": ""},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	r := gin.New()
	h := handler.NewAuthHandler(repository.NewUserRepository(nil), "test-secret", 24)
	r.POST("/internal/auth/validate", h.ValidateToken)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/internal/auth/validate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("want status %d, got %d; body: %s", tt.wantStatusCode, w.Code, w.Body.String())
			}
		})
	}
}
