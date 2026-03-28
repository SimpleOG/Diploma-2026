package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// validateTokenResponse mirrors auth-service's ValidateTokenResponse.
type validateTokenResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// AuthMiddleware returns a Gin middleware that validates JWT tokens via auth-service.
// Results are cached in Redis for 60 seconds to reduce inter-service calls.
func AuthMiddleware(authServiceURL string, redisClient *redis.Client) gin.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header must be Bearer token"})
			return
		}

		token := parts[1]

		// Build Redis cache key using SHA256 hash of the token.
		hash := sha256.Sum256([]byte(token))
		cacheKey := fmt.Sprintf("jwt:%x", hash)

		// Try cache first.
		if redisClient != nil {
			cached, err := redisClient.Get(c.Request.Context(), cacheKey).Result()
			if err == nil {
				var resp validateTokenResponse
				if jsonErr := json.Unmarshal([]byte(cached), &resp); jsonErr == nil {
					c.Set("user_id", resp.UserID)
					c.Set("username", resp.Username)
					c.Next()
					return
				}
			}
		}

		// Call auth-service to validate token.
		resp, err := validateTokenViaAuthService(c.Request.Context(), client, authServiceURL, token)
		if err != nil {
			slog.Warn("token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		// Cache the validated result in Redis.
		if redisClient != nil {
			data, _ := json.Marshal(resp)
			_ = redisClient.Set(c.Request.Context(), cacheKey, data, 60*time.Second).Err()
		}

		c.Set("user_id", resp.UserID)
		c.Set("username", resp.Username)
		c.Next()
	}
}

// validateTokenViaAuthService calls the auth-service internal validation endpoint.
func validateTokenViaAuthService(ctx context.Context, client *http.Client, authServiceURL, token string) (*validateTokenResponse, error) {
	payload := map[string]string{"token": token}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		authServiceURL+"/internal/auth/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call auth service: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth service returned status %d", httpResp.StatusCode)
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var resp validateTokenResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}
