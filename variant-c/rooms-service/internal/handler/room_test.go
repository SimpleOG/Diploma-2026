package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chat-diploma/variant-c/rooms-service/internal/handler"
	"github.com/chat-diploma/variant-c/rooms-service/internal/repository"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupRoomRouter() *gin.Engine {
	roomRepo := repository.NewRoomRepository(nil)
	h := handler.NewRoomHandler(roomRepo, nil)

	r := gin.New()
	// Inject user_id manually for protected routes.
	injectUser := func(c *gin.Context) {
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Set("username", "testuser")
		c.Next()
	}

	r.GET("/api/v1/rooms", injectUser, h.ListRooms)
	r.POST("/api/v1/rooms", injectUser, h.CreateRoom)
	r.POST("/api/v1/rooms/:room_id/join", injectUser, h.JoinRoom)
	r.GET("/internal/rooms/:room_id/members/:user_id", h.CheckMembership)
	return r
}

// TestCreateRoomValidation tests that create room rejects malformed input.
func TestCreateRoomValidation(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		wantStatusCode int
	}{
		{
			name:           "missing name",
			body:           map[string]interface{}{},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "empty name",
			body:           map[string]interface{}{"name": ""},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "name too long",
			body:           map[string]interface{}{"name": string(make([]byte, 101))},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	r := setupRoomRouter()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", bytes.NewReader(body))
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

// TestListRoomsNoDB tests that ListRooms returns 500 when DB is nil (no connection).
func TestListRoomsNoDB(t *testing.T) {
	r := setupRoomRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Without a real DB, expect 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("want status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestCheckMembershipNoDB tests membership check returns 500 with nil DB.
func TestCheckMembershipNoDB(t *testing.T) {
	r := setupRoomRouter()

	req := httptest.NewRequest(http.MethodGet,
		"/internal/rooms/some-room-id/members/some-user-id", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Without a real DB, expect 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("want status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestJoinRoomNoDB tests that JoinRoom returns 500 with nil DB.
func TestJoinRoomNoDB(t *testing.T) {
	r := setupRoomRouter()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/rooms/some-room-id/join", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Without a real DB, expect 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("want status 500, got %d; body: %s", w.Code, w.Body.String())
	}
}
