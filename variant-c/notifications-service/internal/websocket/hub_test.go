package websocket_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	ws "github.com/chat-diploma/variant-c/notifications-service/internal/websocket"
)

// TestHubBroadcastToRoom tests that the hub delivers messages to subscribed clients.
func TestHubBroadcastToRoom(t *testing.T) {
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Give the hub time to start.
	time.Sleep(50 * time.Millisecond)

	// Create a mock client.
	client := ws.NewClient(hub, nil, "user-1", "testuser")

	// Subscribe the client to a room.
	hub.Subscribe(client, "room-1")

	// Give subscription time to register.
	time.Sleep(50 * time.Millisecond)

	// Broadcast a message to the room.
	payload := map[string]string{"type": "new_message", "content": "hello"}
	data, _ := json.Marshal(payload)
	hub.BroadcastToRoom("room-1", data)

	// Give time for delivery.
	time.Sleep(100 * time.Millisecond)

	// Since we can't read from a nil websocket connection directly,
	// verify through the send channel.
	select {
	case received := <-client.SendChan():
		var msg map[string]string
		if err := json.Unmarshal(received, &msg); err != nil {
			t.Fatalf("failed to unmarshal received message: %v", err)
		}
		if msg["type"] != "new_message" {
			t.Errorf("expected type 'new_message', got '%s'", msg["type"])
		}
		if msg["content"] != "hello" {
			t.Errorf("expected content 'hello', got '%s'", msg["content"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout: did not receive broadcast message")
	}
}

// TestHubBroadcastToWrongRoom tests that messages are only delivered to correct room subscribers.
func TestHubBroadcastToWrongRoom(t *testing.T) {
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	client := ws.NewClient(hub, nil, "user-1", "testuser")
	hub.Subscribe(client, "room-1")
	time.Sleep(50 * time.Millisecond)

	// Broadcast to a different room.
	payload := []byte(`{"type":"new_message","content":"not for you"}`)
	hub.BroadcastToRoom("room-2", payload)

	// Verify no message received.
	select {
	case msg := <-client.SendChan():
		t.Errorf("unexpected message received: %s", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected: no message.
	}
}

// TestHubUnsubscribe tests that unsubscribed clients no longer receive messages.
func TestHubUnsubscribe(t *testing.T) {
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	client := ws.NewClient(hub, nil, "user-1", "testuser")
	hub.Subscribe(client, "room-1")
	time.Sleep(50 * time.Millisecond)

	// Unsubscribe.
	hub.Unsubscribe(client, "room-1")
	time.Sleep(50 * time.Millisecond)

	// Broadcast to the room.
	payload := []byte(`{"type":"new_message","content":"after unsubscribe"}`)
	hub.BroadcastToRoom("room-1", payload)

	// Verify no message received.
	select {
	case msg := <-client.SendChan():
		t.Errorf("unexpected message after unsubscribe: %s", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected: no message.
	}
}

// TestHubMultipleClients tests broadcast to multiple clients in same room.
func TestHubMultipleClients(t *testing.T) {
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	client1 := ws.NewClient(hub, nil, "user-1", "alice")
	client2 := ws.NewClient(hub, nil, "user-2", "bob")
	client3 := ws.NewClient(hub, nil, "user-3", "charlie") // different room

	hub.Subscribe(client1, "room-1")
	hub.Subscribe(client2, "room-1")
	hub.Subscribe(client3, "room-2")
	time.Sleep(50 * time.Millisecond)

	payload := []byte(`{"type":"new_message","content":"hello room 1"}`)
	hub.BroadcastToRoom("room-1", payload)

	// Both client1 and client2 should receive the message.
	for _, tc := range []struct {
		name   string
		client *ws.Client
	}{
		{"client1", client1},
		{"client2", client2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			select {
			case <-tc.client.SendChan():
				// OK
			case <-time.After(500 * time.Millisecond):
				t.Errorf("timeout: %s did not receive message", tc.name)
			}
		})
	}

	// client3 should NOT receive the message.
	select {
	case msg := <-client3.SendChan():
		t.Errorf("client3 in room-2 unexpectedly received message: %s", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected.
	}
}
