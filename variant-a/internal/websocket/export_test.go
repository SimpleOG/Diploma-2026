package websocket

// This file exports internal types and methods for use in external (black-box)
// test packages.  The file name ends in _test.go so these exports are only
// compiled during `go test`.

// TestClient wraps a Client with an exported send channel so tests can inspect
// messages without a real WebSocket connection.
type TestClient struct {
	client *Client
}

// NewTestClient constructs a Client suitable for hub unit tests.
// The underlying Client has no WebSocket connection; only the send channel
// and hub fields are populated.
func NewTestClient(hub *Hub, userID, username, roomID string) *TestClient {
	c := &Client{
		userID:   userID,
		username: username,
		roomID:   roomID,
		send:     make(chan []byte, 256),
		hub:      hub,
	}
	return &TestClient{client: c}
}

// Client returns the inner *Client so it can be passed to Hub methods.
func (tc *TestClient) Client() *Client { return tc.client }

// Send returns the client's send channel so tests can read delivered messages.
func (tc *TestClient) Send() <-chan []byte { return tc.client.send }

// RegisterClient exposes hub.register for tests.
func (h *Hub) RegisterClient(c *Client) { h.register <- c }

// UnregisterClient exposes hub.unregister for tests.
func (h *Hub) UnregisterClient(c *Client) { h.unregister <- c }

// HasClient reports whether c is registered in the given room.
func (h *Hub) HasClient(c *Client, roomID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.rooms[roomID]
	if !ok {
		return false
	}
	return clients[c]
}

// RoomCount returns the number of clients in a room (0 if room doesn't exist).
func (h *Hub) RoomCount(roomID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[roomID])
}
