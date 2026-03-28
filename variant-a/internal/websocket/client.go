package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	gorillaws "github.com/gorilla/websocket"
)

const (
	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 65536

	// pongWait is how long we wait for a pong before closing the connection.
	pongWait = 60 * time.Second

	// pingPeriod is how often we send pings (must be less than pongWait).
	pingPeriod = 54 * time.Second

	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second
)

// incomingMessage is the structure of messages received from WebSocket clients.
type incomingMessage struct {
	Type    string `json:"type"`
	RoomID  string `json:"room_id,omitempty"`
	Content string `json:"content,omitempty"`
}

// Client is a middleman between the WebSocket connection and the hub.
type Client struct {
	conn     *gorillaws.Conn
	userID   string
	username string
	roomID   string
	send     chan []byte
	hub      *Hub
}

// NewClient constructs a new Client. The send channel is buffered to 256.
func NewClient(conn *gorillaws.Conn, userID, username string, hub *Hub) *Client {
	return &Client{
		conn:     conn,
		userID:   userID,
		username: username,
		send:     make(chan []byte, 256),
		hub:      hub,
	}
}

// readPump pumps messages from the WebSocket connection to the hub.
// It runs in a per-connection goroutine.
func (c *Client) readPump(msgHandler MessageHandlerInterface) {
	defer func() {
		if c.roomID != "" {
			c.hub.unregister <- c
		}
		if err := c.conn.Close(); err != nil {
			slog.Debug("ws conn close error", "err", err)
		}
	}()

	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		slog.Warn("ws set read deadline failed", "err", err)
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if gorillaws.IsUnexpectedCloseError(err,
				gorillaws.CloseGoingAway,
				gorillaws.CloseAbnormalClosure,
				gorillaws.CloseNormalClosure,
			) {
				slog.Warn("ws unexpected close", "user_id", c.userID, "err", err)
			}
			break
		}

		var msg incomingMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Warn("ws invalid json", "user_id", c.userID, "err", err)
			continue
		}

		switch msg.Type {
		case "join":
			roomID := msg.RoomID
			if roomID == "" {
				slog.Warn("ws join without room_id", "user_id", c.userID)
				continue
			}
			// Only allow joining one room per connection.
			if c.roomID != "" {
				slog.Warn("ws client already in room", "user_id", c.userID, "room_id", c.roomID)
				continue
			}
			c.roomID = roomID
			c.hub.register <- c

			// Notify room members.
			payload, _ := json.Marshal(map[string]string{
				"type":     "user_joined",
				"user_id":  c.userID,
				"username": c.username,
				"room_id":  roomID,
			})
			c.hub.broadcast <- &BroadcastMessage{
				RoomID:  roomID,
				Payload: payload,
				Exclude: c,
			}

		case "message":
			if c.roomID == "" {
				slog.Warn("ws message before join", "user_id", c.userID)
				continue
			}
			roomID := msg.RoomID
			if roomID == "" {
				roomID = c.roomID
			}
			if err := msgHandler.HandleIncoming(c, roomID, msg.Content); err != nil {
				slog.Warn("ws handle incoming error", "user_id", c.userID, "err", err)
			}

		case "ping":
			pong, _ := json.Marshal(map[string]string{"type": "pong"})
			select {
			case c.send <- pong:
			default:
				slog.Warn("ws send channel full on pong", "user_id", c.userID)
			}

		case "leave":
			if c.roomID != "" {
				c.hub.unregister <- c
			}
			return

		default:
			slog.Debug("ws unknown message type", "type", msg.Type, "user_id", c.userID)
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection.
// It runs in a per-connection goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if err := c.conn.Close(); err != nil {
			slog.Debug("ws conn close error in writePump", "err", err)
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				slog.Warn("ws set write deadline failed", "err", err)
				return
			}
			if !ok {
				// Hub closed the channel – send a close frame and return.
				_ = c.conn.WriteMessage(gorillaws.CloseMessage,
					gorillaws.FormatCloseMessage(gorillaws.CloseGoingAway, "server closing"))
				return
			}

			w, err := c.conn.NextWriter(gorillaws.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				slog.Warn("ws write error", "user_id", c.userID, "err", err)
				_ = w.Close()
				return
			}

			// Drain any queued messages in the same writer for efficiency.
			n := len(c.send)
			for i := 0; i < n; i++ {
				if _, err := w.Write(<-c.send); err != nil {
					_ = w.Close()
					return
				}
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(gorillaws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// MessageHandlerInterface abstracts the message handler for testing.
type MessageHandlerInterface interface {
	HandleIncoming(client *Client, roomID, content string) error
}
