package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// validateTokenResponse mirrors auth-service's ValidateTokenResponse.
type validateTokenResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// WSAuth validates the token from the query parameter for WebSocket connections.
// The token is passed as ?token=<jwt> since WebSocket handshakes can't use custom headers.
func WSAuth(authServiceURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token query parameter required"})
			return
		}

		resp, err := validateToken(c.Request.Context(), authServiceURL, token)
		if err != nil {
			slog.Warn("ws token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set("user_id", resp.UserID)
		c.Set("username", resp.Username)
		c.Next()
	}
}

// validateToken calls auth-service to validate a JWT token.
func validateToken(ctx context.Context, authServiceURL, token string) (*validateTokenResponse, error) {
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		authServiceURL+"/internal/auth/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call auth service: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth service returned status %d", httpResp.StatusCode)
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp validateTokenResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}
