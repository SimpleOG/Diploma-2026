package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chat-diploma/variant-a/internal/handler"
	"github.com/chat-diploma/variant-a/internal/middleware"
	"github.com/chat-diploma/variant-a/internal/model"
	"github.com/chat-diploma/variant-a/internal/repository"
	"github.com/gin-gonic/gin"
)

// ── Mocks ────────────────────────────────────────────────────────────────────

type mockRoomRepo struct {
	rooms   map[string]*model.Room
	members map[string]map[string]bool // roomID -> set of userIDs
}

func newMockRoomRepo() *mockRoomRepo {
	return &mockRoomRepo{
		rooms:   make(map[string]*model.Room),
		members: make(map[string]map[string]bool),
	}
}

func (m *mockRoomRepo) Create(_ context.Context, name, ownerID string) (*model.Room, error) {
	id := "room-" + name
	room := &model.Room{
		ID:        id,
		Name:      name,
		OwnerID:   ownerID,
		CreatedAt: time.Now(),
	}
	m.rooms[id] = room
	return room, nil
}

func (m *mockRoomRepo) List(_ context.Context) ([]model.RoomResponse, error) {
	result := make([]model.RoomResponse, 0, len(m.rooms))
	for _, r := range m.rooms {
		cnt := len(m.members[r.ID])
		result = append(result, model.RoomResponse{
			ID:          r.ID,
			Name:        r.Name,
			OwnerID:     r.OwnerID,
			CreatedAt:   r.CreatedAt,
			MemberCount: cnt,
		})
	}
	return result, nil
}

func (m *mockRoomRepo) GetByID(_ context.Context, id string) (*model.Room, error) {
	r, ok := m.rooms[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return r, nil
}

func (m *mockRoomRepo) AddMember(_ context.Context, roomID, userID string) error {
	if _, ok := m.members[roomID]; !ok {
		m.members[roomID] = make(map[string]bool)
	}
	if m.members[roomID][userID] {
		return repository.ErrDuplicate
	}
	m.members[roomID][userID] = true
	return nil
}

func (m *mockRoomRepo) IsMember(_ context.Context, roomID, userID string) (bool, error) {
	return m.members[roomID][userID], nil
}

type mockMsgRepo struct {
	messages map[string][]model.Message
}

func newMockMsgRepo() *mockMsgRepo {
	return &mockMsgRepo{messages: make(map[string][]model.Message)}
}

func (m *mockMsgRepo) ListByRoom(_ context.Context, roomID, before string, limit int) ([]model.Message, bool, error) {
	msgs := m.messages[roomID]
	if len(msgs) == 0 {
		return []model.Message{}, false, nil
	}
	if limit <= 0 {
		limit = 50
	}
	hasMore := false
	if len(msgs) > limit {
		hasMore = true
		msgs = msgs[:limit]
	}
	return msgs, hasMore, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestRoomRouter(roomRepo handler.RoomRepoForTest, msgRepo handler.MsgRepoForTest) *gin.Engine {
	r := gin.New()
	h := handler.NewRoomHandler(roomRepo, msgRepo)

	// Inject a fixed user into context for auth middleware simulation.
	authMock := func(c *gin.Context) {
		c.Set(middleware.ContextUserID, "user-test-1")
		c.Set(middleware.ContextUsername, "testuser")
		c.Next()
	}

	g := r.Group("/rooms")
	g.Use(authMock)
	g.GET("", h.ListRooms)
	g.POST("", h.CreateRoom)
	g.POST("/:id/join", h.JoinRoom)
	g.GET("/:id/messages", h.GetMessages)
	return r
}

func doRequest(t *testing.T, router *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ── ListRooms tests ───────────────────────────────────────────────────────────

func TestListRooms_EmptyReturnsEmptySlice(t *testing.T) {
	roomRepo := newMockRoomRepo()
	router := newTestRoomRouter(roomRepo, newMockMsgRepo())

	w := doRequest(t, router, http.MethodGet, "/rooms", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rooms, ok := resp["rooms"]
	if !ok {
		t.Fatal("response missing 'rooms' key")
	}
	roomSlice, ok := rooms.([]interface{})
	if !ok {
		t.Fatalf("rooms should be array, got %T", rooms)
	}
	if len(roomSlice) != 0 {
		t.Errorf("expected 0 rooms, got %d", len(roomSlice))
	}
}

func TestListRooms_WithRooms(t *testing.T) {
	roomRepo := newMockRoomRepo()
	roomRepo.rooms["r1"] = &model.Room{ID: "r1", Name: "general", OwnerID: "u1"}
	roomRepo.members["r1"] = map[string]bool{"u1": true}

	router := newTestRoomRouter(roomRepo, newMockMsgRepo())
	w := doRequest(t, router, http.MethodGet, "/rooms", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

// ── CreateRoom tests ──────────────────────────────────────────────────────────

func TestCreateRoom(t *testing.T) {
	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
		wantName   string
	}{
		{
			name:       "success",
			body:       map[string]string{"name": "general"},
			wantStatus: http.StatusCreated,
			wantName:   "general",
		},
		{
			name:       "missing name",
			body:       map[string]string{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRoomRouter(newMockRoomRepo(), newMockMsgRepo())
			w := doRequest(t, router, http.MethodPost, "/rooms", tc.body)

			if w.Code != tc.wantStatus {
				t.Errorf("want %d got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			if tc.wantName != "" {
				var resp map[string]interface{}
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				room, _ := resp["room"].(map[string]interface{})
				if room["name"] != tc.wantName {
					t.Errorf("name: want %q got %v", tc.wantName, room["name"])
				}
			}
		})
	}
}

// ── JoinRoom tests ────────────────────────────────────────────────────────────

func TestJoinRoom(t *testing.T) {
	tests := []struct {
		name       string
		setupRepo  func(*mockRoomRepo)
		roomID     string
		wantStatus int
	}{
		{
			name: "success",
			setupRepo: func(r *mockRoomRepo) {
				r.rooms["room-1"] = &model.Room{ID: "room-1", Name: "test", OwnerID: "other"}
			},
			roomID:     "room-1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "room not found",
			setupRepo:  func(r *mockRoomRepo) {},
			roomID:     "nonexistent",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "already member",
			setupRepo: func(r *mockRoomRepo) {
				r.rooms["room-1"] = &model.Room{ID: "room-1", Name: "test", OwnerID: "other"}
				r.members["room-1"] = map[string]bool{"user-test-1": true}
			},
			roomID:     "room-1",
			wantStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roomRepo := newMockRoomRepo()
			tc.setupRepo(roomRepo)
			router := newTestRoomRouter(roomRepo, newMockMsgRepo())

			w := doRequest(t, router, http.MethodPost, "/rooms/"+tc.roomID+"/join", nil)

			if w.Code != tc.wantStatus {
				t.Errorf("want %d got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// ── GetMessages tests ─────────────────────────────────────────────────────────

func TestGetMessages(t *testing.T) {
	tests := []struct {
		name       string
		setupRepos func(*mockRoomRepo, *mockMsgRepo)
		roomID     string
		query      string
		wantStatus int
		wantLen    int
	}{
		{
			name: "success empty",
			setupRepos: func(rr *mockRoomRepo, mr *mockMsgRepo) {
				rr.rooms["room-1"] = &model.Room{ID: "room-1", Name: "t", OwnerID: "u"}
				rr.members["room-1"] = map[string]bool{"user-test-1": true}
			},
			roomID:     "room-1",
			wantStatus: http.StatusOK,
			wantLen:    0,
		},
		{
			name: "success with messages",
			setupRepos: func(rr *mockRoomRepo, mr *mockMsgRepo) {
				rr.rooms["room-1"] = &model.Room{ID: "room-1", Name: "t", OwnerID: "u"}
				rr.members["room-1"] = map[string]bool{"user-test-1": true}
				mr.messages["room-1"] = []model.Message{
					{ID: "msg-1", RoomID: "room-1", Content: "hello"},
					{ID: "msg-2", RoomID: "room-1", Content: "world"},
				}
			},
			roomID:     "room-1",
			wantStatus: http.StatusOK,
			wantLen:    2,
		},
		{
			name:       "room not found",
			setupRepos: func(rr *mockRoomRepo, mr *mockMsgRepo) {},
			roomID:     "nonexistent",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "not a member",
			setupRepos: func(rr *mockRoomRepo, mr *mockMsgRepo) {
				rr.rooms["room-1"] = &model.Room{ID: "room-1", Name: "t", OwnerID: "other"}
				// user-test-1 is not a member
			},
			roomID:     "room-1",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roomRepo := newMockRoomRepo()
			msgRepo := newMockMsgRepo()
			tc.setupRepos(roomRepo, msgRepo)
			router := newTestRoomRouter(roomRepo, msgRepo)

			path := "/rooms/" + tc.roomID + "/messages"
			if tc.query != "" {
				path += "?" + tc.query
			}
			w := doRequest(t, router, http.MethodGet, path, nil)

			if w.Code != tc.wantStatus {
				t.Errorf("want %d got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			if tc.wantStatus == http.StatusOK {
				var resp model.MessageResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(resp.Messages) != tc.wantLen {
					t.Errorf("messages: want %d got %d", tc.wantLen, len(resp.Messages))
				}
			}
		})
	}
}
