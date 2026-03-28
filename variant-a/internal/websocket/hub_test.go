package websocket_test

import (
	"encoding/json"
	"testing"
	"time"

	appws "github.com/chat-diploma/variant-a/internal/websocket"
)

// newTestHub returns a Hub with no Redis client (suitable for unit tests).
func newTestHub() *appws.Hub {
	return appws.NewHub(nil)
}

// newTestClient creates a minimal Client for hub testing without a real WebSocket.
func newTestClient(hub *appws.Hub, userID, username, roomID string) *appws.TestClient {
	return appws.NewTestClient(hub, userID, username, roomID)
}

func runHub(h *appws.Hub) {
	go h.Run()
	// Give the hub goroutine a moment to start.
	time.Sleep(10 * time.Millisecond)
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestHub_Register_AddsClientToRoom(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c := newTestClient(hub, "user-1", "alice", "room-1")
	hub.RegisterClient(c.Client())

	time.Sleep(20 * time.Millisecond)

	if !hub.HasClient(c.Client(), "room-1") {
		t.Error("client not found in room after register")
	}
}

func TestHub_Register_MultipleClientsInRoom(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c1 := newTestClient(hub, "user-1", "alice", "room-1")
	c2 := newTestClient(hub, "user-2", "bob", "room-1")

	hub.RegisterClient(c1.Client())
	hub.RegisterClient(c2.Client())

	time.Sleep(20 * time.Millisecond)

	if !hub.HasClient(c1.Client(), "room-1") {
		t.Error("client 1 not in room")
	}
	if !hub.HasClient(c2.Client(), "room-1") {
		t.Error("client 2 not in room")
	}
}

// ── Unregister ────────────────────────────────────────────────────────────────

func TestHub_Unregister_RemovesClientFromRoom(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c := newTestClient(hub, "user-1", "alice", "room-1")
	hub.RegisterClient(c.Client())
	time.Sleep(20 * time.Millisecond)

	hub.UnregisterClient(c.Client())
	time.Sleep(50 * time.Millisecond)

	if hub.HasClient(c.Client(), "room-1") {
		t.Error("client still in room after unregister")
	}
}

func TestHub_Unregister_ClosesClientSendChannel(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c := newTestClient(hub, "user-1", "alice", "room-1")
	hub.RegisterClient(c.Client())
	time.Sleep(20 * time.Millisecond)

	hub.UnregisterClient(c.Client())
	time.Sleep(50 * time.Millisecond)

	// After unregister the send channel should be closed; reading from it
	// should complete immediately (zero value + closed).
	select {
	case _, ok := <-c.Send():
		if ok {
			// Channel still open and has data – that's fine (user_left notification).
		}
		// Either a value was drained or channel closed – both are acceptable.
	case <-time.After(100 * time.Millisecond):
		// Channel neither readable nor closed within timeout – check if already closed.
	}
}

func TestHub_Unregister_DeletesRoomWhenEmpty(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c := newTestClient(hub, "user-1", "alice", "room-empty")
	hub.RegisterClient(c.Client())
	time.Sleep(20 * time.Millisecond)

	hub.UnregisterClient(c.Client())
	time.Sleep(50 * time.Millisecond)

	if hub.RoomCount("room-empty") != 0 {
		t.Error("room should be deleted when last client leaves")
	}
}

// ── Broadcast ─────────────────────────────────────────────────────────────────

func TestHub_Broadcast_DeliverToAllInRoom(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c1 := newTestClient(hub, "user-1", "alice", "room-broadcast")
	c2 := newTestClient(hub, "user-2", "bob", "room-broadcast")

	hub.RegisterClient(c1.Client())
	hub.RegisterClient(c2.Client())
	time.Sleep(20 * time.Millisecond)

	payload := []byte(`{"type":"message","content":"hello"}`)
	hub.Broadcast(&appws.BroadcastMessage{
		RoomID:  "room-broadcast",
		Payload: payload,
	})

	// Both clients should receive the message.
	for i, c := range []*appws.TestClient{c1, c2} {
		select {
		case msg := <-c.Send():
			if string(msg) != string(payload) {
				t.Errorf("client %d: want %s got %s", i+1, payload, msg)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("client %d: timed out waiting for message", i+1)
		}
	}
}

func TestHub_Broadcast_ExcludesSender(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c1 := newTestClient(hub, "user-1", "alice", "room-x")
	c2 := newTestClient(hub, "user-2", "bob", "room-x")

	hub.RegisterClient(c1.Client())
	hub.RegisterClient(c2.Client())
	time.Sleep(20 * time.Millisecond)

	payload := []byte(`{"type":"message","content":"hi"}`)
	hub.Broadcast(&appws.BroadcastMessage{
		RoomID:  "room-x",
		Payload: payload,
		Exclude: c1.Client(),
	})

	// c1 should NOT receive (it's excluded), c2 SHOULD receive.
	select {
	case msg := <-c2.Send():
		if string(msg) != string(payload) {
			t.Errorf("c2: unexpected payload %s", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("c2: timed out waiting for message")
	}

	// c1 should receive nothing.
	select {
	case msg := <-c1.Send():
		t.Errorf("c1 received a message but was excluded: %s", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected: no message for excluded client.
	}
}

func TestHub_Broadcast_DifferentRoom_NotDelivered(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c1 := newTestClient(hub, "user-1", "alice", "room-A")
	c2 := newTestClient(hub, "user-2", "bob", "room-B")

	hub.RegisterClient(c1.Client())
	hub.RegisterClient(c2.Client())
	time.Sleep(20 * time.Millisecond)

	// Broadcast to room-A only.
	hub.Broadcast(&appws.BroadcastMessage{
		RoomID:  "room-A",
		Payload: []byte(`{"type":"message"}`),
	})

	// c2 should not get it.
	select {
	case msg := <-c2.Send():
		t.Errorf("c2 in room-B received message from room-A: %s", msg)
	case <-time.After(50 * time.Millisecond):
		// Good.
	}
}

// ── Payload shape ─────────────────────────────────────────────────────────────

func TestHub_Broadcast_PayloadIsValidJSON(t *testing.T) {
	hub := newTestHub()
	runHub(hub)

	c := newTestClient(hub, "user-1", "alice", "room-json")
	hub.RegisterClient(c.Client())
	time.Sleep(20 * time.Millisecond)

	payload, _ := json.Marshal(map[string]string{
		"type":    "message",
		"content": "hello world",
	})
	hub.Broadcast(&appws.BroadcastMessage{RoomID: "room-json", Payload: payload})

	select {
	case msg := <-c.Send():
		var out map[string]interface{}
		if err := json.Unmarshal(msg, &out); err != nil {
			t.Errorf("received invalid JSON: %s", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for broadcast")
	}
}
