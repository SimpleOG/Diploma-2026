package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"
)

// subscription tracks which rooms a client is subscribed to.
type subscription struct {
	client *Client
	roomID string
}

// Hub manages WebSocket clients and room subscriptions.
// It uses Redis pub/sub to coordinate across multiple service instances.
type Hub struct {
	// clients maps room_id -> set of clients.
	rooms map[string]map[*Client]struct{}
	// clients maps client -> set of room_ids.
	clientRooms map[*Client]map[string]struct{}

	register   chan subscription
	unregister chan subscription
	broadcast  chan roomMessage

	mu          sync.RWMutex
	redisClient *redis.Client
	redisPubSub *redis.PubSub
}

// roomMessage is an internal broadcast request.
type roomMessage struct {
	roomID  string
	payload []byte
}

// NewHub creates a new Hub.
func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		rooms:       make(map[string]map[*Client]struct{}),
		clientRooms: make(map[*Client]map[string]struct{}),
		register:    make(chan subscription, 256),
		unregister:  make(chan subscription, 256),
		broadcast:   make(chan roomMessage, 512),
		redisClient: redisClient,
	}
}

// Run starts the hub event loop. Should be called in a goroutine.
func (h *Hub) Run(ctx context.Context) {
	var redisCh <-chan *redis.Message

	// Subscribe to all room channels via Redis if available.
	if h.redisClient != nil {
		h.redisPubSub = h.redisClient.PSubscribe(ctx, "room:*")
		redisCh = h.redisPubSub.Channel()
	}

	for {
		select {
		case <-ctx.Done():
			if h.redisPubSub != nil {
				_ = h.redisPubSub.Close()
			}
			return

		case sub := <-h.register:
			h.mu.Lock()
			if h.rooms[sub.roomID] == nil {
				h.rooms[sub.roomID] = make(map[*Client]struct{})
			}
			h.rooms[sub.roomID][sub.client] = struct{}{}

			if h.clientRooms[sub.client] == nil {
				h.clientRooms[sub.client] = make(map[string]struct{})
			}
			h.clientRooms[sub.client][sub.roomID] = struct{}{}
			h.mu.Unlock()

		case sub := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.rooms[sub.roomID]; ok {
				delete(clients, sub.client)
				if len(clients) == 0 {
					delete(h.rooms, sub.roomID)
				}
			}
			if rooms, ok := h.clientRooms[sub.client]; ok {
				delete(rooms, sub.roomID)
				if len(rooms) == 0 {
					delete(h.clientRooms, sub.client)
				}
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			// Broadcast locally to all clients in the room.
			h.broadcastLocal(msg.roomID, msg.payload)
			// Also publish to Redis so other instances can forward it.
			h.publishToRedis(ctx, msg.roomID, msg.payload)

		case redisMsg, ok := <-redisCh:
			if !ok {
				return
			}
			// Parse the Redis message to determine the room.
			var envelope redisEnvelope
			if err := json.Unmarshal([]byte(redisMsg.Payload), &envelope); err != nil {
				slog.Warn("failed to decode redis message", "error", err)
				continue
			}
			// Broadcast locally only (avoid infinite loop).
			h.broadcastLocal(envelope.RoomID, []byte(redisMsg.Payload))
		}
	}
}

// redisEnvelope wraps the payload with room routing info.
type redisEnvelope struct {
	RoomID  string          `json:"room_id"`
	Payload json.RawMessage `json:"payload"`
}

// broadcastLocal sends a message to all local clients in the room.
func (h *Hub) broadcastLocal(roomID string, data []byte) {
	h.mu.RLock()
	clients := h.rooms[roomID]
	h.mu.RUnlock()

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// Client is slow - remove it.
			slog.Warn("dropping slow websocket client", "room_id", roomID)
		}
	}
}

// publishToRedis publishes a message to the Redis pub/sub channel for the room.
func (h *Hub) publishToRedis(ctx context.Context, roomID string, payload []byte) {
	if h.redisClient == nil {
		return
	}

	envelope := redisEnvelope{
		RoomID:  roomID,
		Payload: payload,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		slog.Error("failed to marshal redis envelope", "error", err)
		return
	}

	channel := fmt.Sprintf("room:%s", roomID)
	if err := h.redisClient.Publish(ctx, channel, data).Err(); err != nil {
		slog.Error("failed to publish to redis", "channel", channel, "error", err)
	}
}

// BroadcastToRoom queues a message to be sent to all clients in a room.
func (h *Hub) BroadcastToRoom(roomID string, payload []byte) {
	select {
	case h.broadcast <- roomMessage{roomID: roomID, payload: payload}:
	default:
		slog.Warn("hub: broadcast channel full, dropping user_left notification",
			"room_id", roomID)
	}
}

// Subscribe registers a client to receive messages for a room.
func (h *Hub) Subscribe(client *Client, roomID string) {
	h.register <- subscription{client: client, roomID: roomID}
}

// Unsubscribe removes a client from a room's subscriber list.
func (h *Hub) Unsubscribe(client *Client, roomID string) {
	h.unregister <- subscription{client: client, roomID: roomID}
}

// UnsubscribeAll removes a client from all rooms.
func (h *Hub) UnsubscribeAll(client *Client) {
	h.mu.RLock()
	rooms, ok := h.clientRooms[client]
	if !ok {
		h.mu.RUnlock()
		return
	}
	// Copy room IDs to avoid holding read lock while sending.
	roomIDs := make([]string, 0, len(rooms))
	for roomID := range rooms {
		roomIDs = append(roomIDs, roomID)
	}
	h.mu.RUnlock()

	for _, roomID := range roomIDs {
		h.unregister <- subscription{client: client, roomID: roomID}
	}
}
