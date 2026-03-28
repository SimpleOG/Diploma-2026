package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chat-diploma/variant-a/internal/auth"
	"github.com/chat-diploma/variant-a/internal/handler"
	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/chat-diploma/variant-a/internal/repository"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ── Mocks ────────────────────────────────────────────────────────────────────

// mockUserRepo is a simple in-memory user store for testing.
type mockUserRepo struct {
	users map[string]*model.User // keyed by username
	// createErr is returned instead of creating a user.
	createErr error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*model.User)}
}

func (m *mockUserRepo) Create(_ context.Context, username, hashedPassword string) (*model.User, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if _, exists := m.users[username]; exists {
		// Simulate duplicate username.
		return nil, errors.New("pq: duplicate key value violates unique constraint")
	}
	u := &model.User{
		ID:        "test-id-" + username,
		Username:  username,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
	}
	m.users[username] = u
	return u, nil
}

func (m *mockUserRepo) GetByUsername(_ context.Context, username string) (*model.User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return u, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestAuthRouter(userRepo handler.UserRepoForTest, authSvc *auth.Service) *gin.Engine {
	r := gin.New()
	h := handler.NewAuthHandler(userRepo, authSvc)
	r.POST("/auth/register", h.Register)
	r.POST("/auth/login", h.Login)
	return r
}

func postJSON(t *testing.T, router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ── Register tests ────────────────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	tests := []struct {
		name       string
		body       interface{}
		setupRepo  func(*mockUserRepo)
		wantStatus int
		wantToken  bool
	}{
		{
			name:       "success",
			body:       map[string]string{"username": "alice", "password": "securepass"},
			wantStatus: http.StatusCreated,
			wantToken:  true,
		},
		{
			name:       "missing username",
			body:       map[string]string{"password": "securepass"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing password",
			body:       map[string]string{"username": "alice"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "username too short",
			body:       map[string]string{"username": "ab", "password": "securepass"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "password too short",
			body:       map[string]string{"username": "alice", "password": "ab"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate username",
			body: map[string]string{"username": "alice", "password": "securepass"},
			setupRepo: func(m *mockUserRepo) {
				// Pre-populate to simulate duplicate.
				m.users["alice"] = &model.User{ID: "existing", Username: "alice"}
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "invalid json",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockUserRepo()
			if tc.setupRepo != nil {
				tc.setupRepo(repo)
			}
			authSvc := auth.NewService("test-secret", 24)
			router := newTestAuthRouter(repo, authSvc)

			w := postJSON(t, router, "/auth/register", tc.body)

			if w.Code != tc.wantStatus {
				t.Errorf("status: want %d got %d; body: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			if tc.wantToken {
				var resp model.AuthResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if resp.Token == "" {
					t.Error("expected non-empty token in response")
				}
				if resp.UserID == "" {
					t.Error("expected non-empty user_id in response")
				}
				if resp.Username != "alice" {
					t.Errorf("username: want alice got %s", resp.Username)
				}
			}
		})
	}
}

// ── Login tests ───────────────────────────────────────────────────────────────

func TestLogin(t *testing.T) {
	authSvc := auth.NewService("test-secret", 24)

	// Pre-hash a password for the seed user.
	hashed, err := authSvc.HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	seedRepo := func() *mockUserRepo {
		repo := newMockUserRepo()
		repo.users["alice"] = &model.User{
			ID:       "user-alice",
			Username: "alice",
			Password: hashed,
		}
		return repo
	}

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
		wantToken  bool
	}{
		{
			name:       "success",
			body:       map[string]string{"username": "alice", "password": "correct-password"},
			wantStatus: http.StatusOK,
			wantToken:  true,
		},
		{
			name:       "wrong password",
			body:       map[string]string{"username": "alice", "password": "wrong-password"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "unknown user",
			body:       map[string]string{"username": "bob", "password": "any"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing username",
			body:       map[string]string{"password": "pass"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing password",
			body:       map[string]string{"username": "alice"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestAuthRouter(seedRepo(), authSvc)
			w := postJSON(t, router, "/auth/login", tc.body)

			if w.Code != tc.wantStatus {
				t.Errorf("status: want %d got %d; body: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			if tc.wantToken {
				var resp model.AuthResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if resp.Token == "" {
					t.Error("expected non-empty token")
				}
				if resp.UserID != "user-alice" {
					t.Errorf("user_id: want user-alice got %s", resp.UserID)
				}
			}
		})
	}
}
