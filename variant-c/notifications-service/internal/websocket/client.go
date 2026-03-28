package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chat-diploma/variant-c/notifications-service/internal/model"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer.
	maxMessageSize = 4096
)

// Client represents a single WebSocket connection.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	userID   string
	username string
}

// NewClient creates a new Client.
func NewClient(hub *Hub, conn *websocket.Conn, userID, username string) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   userID,
		username: username,
	}
}

// SendChan returns the client's send channel (used for testing).
func (c *Client) SendChan() <-chan []byte {
	return c.send
}

// ReadPump pumps messages from the WebSocket connection to the hub.
// Handles "join", "leave", and "ping" events from the client.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.UnsubscribeAll(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, rawMsg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure) {
				slog.Warn("websocket read error", "user_id", c.userID, "error", err)
			}
			return
		}

		var msg model.WSIncoming
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			slog.Warn("failed to parse ws message", "error", err)
			continue
		}

		switch msg.Type {
		case "join":
			if msg.RoomID == "" {
				continue
			}
			c.hub.Subscribe(c, msg.RoomID)
			resp, _ := json.Marshal(map[string]string{
				"type":    "joined",
				"room_id": msg.RoomID,
			})
			select {
			case c.send <- resp:
			default:
			}

		case "leave":
			if msg.RoomID == "" {
				continue
			}
			c.hub.Unsubscribe(c, msg.RoomID)
			resp, _ := json.Marshal(map[string]string{
				"type":    "left",
				"room_id": msg.RoomID,
			})
			select {
			case c.send <- resp:
			default:
			}

		case "ping":
			resp, _ := json.Marshal(map[string]string{"type": "pong"})
			select {
			case c.send <- resp:
			default:
			}

		default:
			slog.Debug("unknown ws message type", "type", msg.Type)
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel closed by hub.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(msg)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
