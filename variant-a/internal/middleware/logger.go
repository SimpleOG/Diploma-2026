package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger returns a Gin middleware that logs each HTTP request using slog.
// It records the HTTP method, path, status code, latency in milliseconds, the
// remote IP, and the authenticated user ID when present in the context.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latencyMs := time.Since(start).Milliseconds()

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", latencyMs),
			slog.String("ip", c.ClientIP()),
		}

		if userID, exists := c.Get(ContextUserID); exists {
			if uid, ok := userID.(string); ok && uid != "" {
				attrs = append(attrs, slog.String("user_id", uid))
			}
		}

		level := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		} else if c.Writer.Status() >= 400 {
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}
