package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// HealthHandler performs readiness checks for downstream dependencies.
type HealthHandler struct {
	db          *sql.DB
	redisClient *redis.Client
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(db *sql.DB, redisClient *redis.Client) *HealthHandler {
	return &HealthHandler{
		db:          db,
		redisClient: redisClient,
	}
}

// Health handles GET /health.
// Returns 200 {"status":"ok"} when both PostgreSQL and Redis are reachable,
// or 503 {"status":"unhealthy","details":{...}} otherwise.
func (h *HealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	details := gin.H{}
	healthy := true

	// Ping PostgreSQL.
	if err := h.db.PingContext(ctx); err != nil {
		details["postgres"] = err.Error()
		healthy = false
	} else {
		details["postgres"] = "ok"
	}

	// Ping Redis.
	if err := h.redisClient.Ping(ctx).Err(); err != nil {
		details["redis"] = err.Error()
		healthy = false
	} else {
		details["redis"] = "ok"
	}

	if healthy {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "details": details})
		return
	}

	c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "details": details})
}
