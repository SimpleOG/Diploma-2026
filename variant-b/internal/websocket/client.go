package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 65536
)

// Client is a middleman between the WebSocket connection and the hub.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	roomID   string
	userID   string
	username string
	handler  *Handler
}

// NewClient creates and registers a new Client for the given connection.
func NewClient(hub *Hub, conn *websocket.Conn, roomID, userID, username string, handler *Handler) *Client {
	c := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		roomID:   roomID,
		userID:   userID,
		username: username,
		handler:  handler,
	}
	hub.register <- c
	return c
}

// ReadPump pumps messages from the WebSocket connection to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		slog.Error("ws: set read deadline", "error", err)
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, rawMsg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure,
			) {
				slog.Warn("ws: read error", "user", c.userID, "error", err)
			}
			break
		}

		c.handler.HandleIncoming(c, rawMsg)
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
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				slog.Error("ws: set write deadline", "error", err)
				return
			}
			if !ok {
				// Hub closed the channel – send CloseGoingAway (1001) per TZ spec.
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseGoingAway, "server closing"))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("ws: write error", "user", c.userID, "error", err)
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendJSON encodes v as JSON and enqueues it for delivery to this client.
func (c *Client) SendJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("ws: marshal outbound message", "error", err)
		return
	}
	select {
	case c.send <- data:
	default:
		slog.Warn("ws: send buffer full, dropping message", "user", c.userID)
	}
}
