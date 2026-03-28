package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// validateTokenResponse mirrors auth-service's ValidateTokenResponse.
type validateTokenResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// AuthMiddleware returns a Gin middleware that validates JWT tokens via auth-service.
// Implements 3 retries with exponential backoff (100ms, 200ms, 400ms).
func AuthMiddleware(authServiceURL string) gin.HandlerFunc {
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

		resp, err := validateWithRetry(c.Request.Context(), client, authServiceURL, token)
		if err != nil {
			slog.Warn("token validation failed", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set("user_id", resp.UserID)
		c.Set("username", resp.Username)
		c.Next()
	}
}

// validateWithRetry calls auth-service with exponential backoff: 100ms, 200ms, 400ms.
func validateWithRetry(ctx context.Context, client *http.Client, authServiceURL, token string) (*validateTokenResponse, error) {
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	var lastErr error

	for attempt := 0; attempt <= len(backoffs); attempt++ {
		resp, err := callAuthService(ctx, client, authServiceURL, token)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if attempt < len(backoffs) {
			select {
			case <-time.After(backoffs[attempt]):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// callAuthService makes a single HTTP call to auth-service.
func callAuthService(ctx context.Context, client *http.Client, authServiceURL, token string) (*validateTokenResponse, error) {
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

	if httpResp.StatusCode == http.StatusUnauthorized {
		// Don't retry on explicit 401 - token is invalid.
		return nil, fmt.Errorf("unauthorized")
	}

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
