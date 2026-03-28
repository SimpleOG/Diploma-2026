package websocket

import (
	"log/slog"
	"sync"
)

// Hub maintains the set of active clients and broadcasts messages to rooms.
type Hub struct {
	// rooms maps roomID -> set of clients.
	rooms map[string]map[*Client]struct{}
	mu    sync.RWMutex

	register   chan *Client
	unregister chan *Client
	broadcast  chan *BroadcastMessage
}

// BroadcastMessage is a message to be sent to all clients in a room.
type BroadcastMessage struct {
	RoomID  string
	Payload []byte
}

// NewHub creates and returns a new Hub.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]struct{}),
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
		broadcast:  make(chan *BroadcastMessage, 1024),
	}
}

// Run starts the hub event loop. It should be called in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if _, ok := h.rooms[client.roomID]; !ok {
				h.rooms[client.roomID] = make(map[*Client]struct{})
			}
			h.rooms[client.roomID][client] = struct{}{}
			h.mu.Unlock()
			slog.Debug("ws: client registered", "room", client.roomID, "user", client.userID)

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.rooms[client.roomID]; ok {
				delete(clients, client)
				if len(clients) == 0 {
					delete(h.rooms, client.roomID)
				}
			}
			h.mu.Unlock()
			close(client.send)
			slog.Debug("ws: client unregistered", "room", client.roomID, "user", client.userID)

		case msg := <-h.broadcast:
			h.mu.RLock()
			clients := h.rooms[msg.RoomID]
			h.mu.RUnlock()

			for client := range clients {
				select {
				case client.send <- msg.Payload:
				default:
					// Slow client – drop the message to avoid blocking the hub.
					slog.Warn("ws: dropped message for slow client",
						"room", msg.RoomID, "user", client.userID)
				}
			}
		}
	}
}

// Broadcast enqueues a message to be sent to all clients in the room.
func (h *Hub) Broadcast(roomID string, payload []byte) {
	h.broadcast <- &BroadcastMessage{RoomID: roomID, Payload: payload}
}
