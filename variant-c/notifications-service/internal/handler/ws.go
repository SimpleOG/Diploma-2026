package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	ws "github.com/chat-diploma/variant-c/notifications-service/internal/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins. In production, restrict to known origins.
		return true
	},
}

// WSHandler handles WebSocket upgrade and client lifecycle.
type WSHandler struct {
	hub *ws.Hub
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(hub *ws.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// ServeWS handles GET /ws.
// Upgrades the connection to WebSocket, creates a client, and starts pumps.
func (h *WSHandler) ServeWS(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user identity"})
		return
	}

	username, _ := c.Get("username")
	usernameStr, _ := username.(string)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Warn("failed to upgrade websocket", "user_id", userIDStr, "error", err)
		return
	}

	client := ws.NewClient(h.hub, conn, userIDStr, usernameStr)

	// Start write pump in background.
	go client.WritePump()
	// Read pump runs in the current goroutine (blocking).
	client.ReadPump()
}
