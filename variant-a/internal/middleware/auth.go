package middleware

import (
	"net/http"
	"strings"

	"github.com/chat-diploma/variant-a/internal/auth"
	"github.com/gin-gonic/gin"
)

const (
	// ContextUserID is the gin context key for the authenticated user's ID.
	ContextUserID = "user_id"
	// ContextUsername is the gin context key for the authenticated user's username.
	ContextUsername = "username"
)

// Auth returns a Gin middleware that validates a Bearer JWT from the
// Authorization header and stores user_id / username in the context.
func Auth(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		userID, username, err := authSvc.ValidateToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ContextUserID, userID)
		c.Set(ContextUsername, username)
		c.Next()
	}
}

// WebSocketAuth returns a Gin middleware that validates a JWT supplied as the
// "token" query parameter. This is required because browser WebSocket APIs do
// not support custom request headers.
func WebSocketAuth(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := c.Query("token")
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token query parameter required"})
			return
		}

		userID, username, err := authSvc.ValidateToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ContextUserID, userID)
		c.Set(ContextUsername, username)
		c.Next()
	}
}
