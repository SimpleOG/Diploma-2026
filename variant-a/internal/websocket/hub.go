package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"
)

// BroadcastMessage carries a payload destined for all clients in a room,
// optionally excluding one sender client.
type BroadcastMessage struct {
	RoomID  string
	Payload []byte
	Exclude *Client
}

// Hub manages all active WebSocket clients and routes messages between them.
// It also integrates with Redis pub/sub so that messages published from other
// server instances are forwarded to local clients.
type Hub struct {
	// rooms maps roomID → set of connected clients.
	rooms map[string]map[*Client]bool

	register   chan *Client
	unregister chan *Client
	broadcast  chan *BroadcastMessage

	redisClient *redis.Client
	mu          sync.RWMutex
}

// NewHub constructs a Hub.  redisClient may be nil for tests that don't need
// pub/sub.
func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		rooms:       make(map[string]map[*Client]bool),
		register:    make(chan *Client, 256),
		unregister:  make(chan *Client, 256),
		broadcast:   make(chan *BroadcastMessage, 512),
		redisClient: redisClient,
	}
}

// Run starts the hub's main event loop.  It must be called in its own
// goroutine.  It also starts the Redis subscriber goroutine.
func (h *Hub) Run() {
	if h.redisClient != nil {
		go h.subscribeRedis()
	}

	for {
		select {
		case client := <-h.register:
			h.handleRegister(client)

		case client := <-h.unregister:
			h.handleUnregister(client)

		case msg := <-h.broadcast:
			h.handleBroadcast(msg)
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[client.roomID] == nil {
		h.rooms[client.roomID] = make(map[*Client]bool)
	}
	h.rooms[client.roomID][client] = true

	slog.Info("ws client registered", "user_id", client.userID, "room_id", client.roomID)
}

func (h *Hub) handleUnregister(client *Client) {
	h.mu.Lock()

	if clients, ok := h.rooms[client.roomID]; ok {
		if _, exists := clients[client]; exists {
			delete(clients, client)
			close(client.send)

			if len(clients) == 0 {
				delete(h.rooms, client.roomID)
			}
		}
	}

	h.mu.Unlock()

	// Notify remaining room members that this user left.
	if client.roomID != "" {
		payload, _ := json.Marshal(map[string]string{
			"type":     "user_left",
			"user_id":  client.userID,
			"username": client.username,
			"room_id":  client.roomID,
		})
		h.broadcast <- &BroadcastMessage{
			RoomID:  client.roomID,
			Payload: payload,
			Exclude: client,
		}
	}

	slog.Info("ws client unregistered", "user_id", client.userID, "room_id", client.roomID)
}

func (h *Hub) handleBroadcast(msg *BroadcastMessage) {
	h.mu.RLock()
	clients := h.rooms[msg.RoomID]
	h.mu.RUnlock()

	for client := range clients {
		if client == msg.Exclude {
			continue
		}
		select {
		case client.send <- msg.Payload:
		default:
			// Slow client – unregister asynchronously to avoid deadlock.
			go func(c *Client) { h.unregister <- c }(client)
		}
	}

	// Also publish to Redis so other server instances can forward the message.
	if h.redisClient != nil {
		channel := fmt.Sprintf("room:%s", msg.RoomID)
		if err := h.redisClient.Publish(context.Background(), channel, msg.Payload).Err(); err != nil {
			slog.Warn("redis publish failed", "channel", channel, "err", err)
		}
	}
}

// subscribeRedis subscribes to the pattern "room:*" and forwards received
// messages to local clients.
func (h *Hub) subscribeRedis() {
	ctx := context.Background()
	pubsub := h.redisClient.PSubscribe(ctx, "room:*")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		// Extract roomID from channel name "room:{roomID}".
		var roomID string
		if len(msg.Channel) > 5 {
			roomID = msg.Channel[5:]
		}
		if roomID == "" {
			continue
		}

		payload := []byte(msg.Payload)

		h.mu.RLock()
		clients := h.rooms[roomID]
		h.mu.RUnlock()

		for client := range clients {
			select {
			case client.send <- payload:
			default:
				go func(c *Client) { h.unregister <- c }(client)
			}
		}
	}
}

// Broadcast enqueues a message for delivery to all room clients.
func (h *Hub) Broadcast(msg *BroadcastMessage) {
	h.broadcast <- msg
}
