package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chat-diploma/variant-c/messages-service/internal/handler"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupMessageRouter(roomsServiceURL string) *gin.Engine {
	h := handler.NewMessageHandler(nil, nil, roomsServiceURL)

	r := gin.New()
	injectUser := func(c *gin.Context) {
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Set("username", "testuser")
		c.Next()
	}

	r.POST("/api/v1/messages", injectUser, h.SendMessage)
	r.GET("/api/v1/rooms/:room_id/messages", injectUser, h.ListMessages)
	return r
}

// TestSendMessageValidation tests that the send message endpoint rejects invalid input.
func TestSendMessageValidation(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]interface{}
		wantStatusCode int
	}{
		{
			name:           "missing room_id",
			body:           map[string]interface{}{"content": "hello"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "missing content",
			body:           map[string]interface{}{"room_id": "some-room"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "empty body",
			body:           map[string]interface{}{},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "content too long",
			body: map[string]interface{}{
				"room_id": "some-room",
				"content": string(make([]byte, 4097)),
			},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	// Use a mock rooms service that always denies membership.
	mockRooms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer mockRooms.Close()

	r := setupMessageRouter(mockRooms.URL)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatusCode {
				t.Errorf("want status %d, got %d; body: %s", tt.wantStatusCode, w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("response is not valid JSON: %s", w.Body.String())
			}
			if _, ok := resp["error"]; !ok {
				t.Error("expected 'error' key in response body")
			}
		})
	}
}

// TestSendMessageNotMember tests that a non-member cannot send a message.
func TestSendMessageNotMember(t *testing.T) {
	// Mock rooms-service that returns 403.
	mockRooms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "not a member"})
	}))
	defer mockRooms.Close()

	r := setupMessageRouter(mockRooms.URL)

	body, _ := json.Marshal(map[string]interface{}{
		"room_id": "some-room-id",
		"content": "hello world",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want status 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestListMessagesEmptyRoomID tests that list messages requires a room_id.
func TestListMessagesEmptyRoomID(t *testing.T) {
	mockRooms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer mockRooms.Close()

	r := setupMessageRouter(mockRooms.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/test-room/messages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Rooms service denies, so expect 500 (membership check error) or 403.
	if w.Code == http.StatusOK {
		t.Errorf("did not expect 200 response without valid membership")
	}
}
