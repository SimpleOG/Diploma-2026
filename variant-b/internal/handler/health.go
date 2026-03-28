package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// HealthHandler handles infrastructure health checks.
type HealthHandler struct {
	db      *sql.DB
	mongoDB *mongo.Database
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *sql.DB, mongoDB *mongo.Database) *HealthHandler {
	return &HealthHandler{db: db, mongoDB: mongoDB}
}

// Health handles GET /health.
func (h *HealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	status := gin.H{
		"status":    "ok",
		"postgres":  "ok",
		"mongodb":   "ok",
	}
	httpStatus := http.StatusOK

	// Ping PostgreSQL.
	if err := h.db.PingContext(ctx); err != nil {
		status["postgres"] = "error: " + err.Error()
		status["status"] = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	// Ping MongoDB via a lightweight ping command.
	if err := h.mongoDB.Client().Ping(ctx, nil); err != nil {
		status["mongodb"] = "error: " + err.Error()
		status["status"] = "degraded"
		httpStatus = http.StatusServiceUnavailable
	} else {
		// Run a no-op command to confirm the database is accessible.
		if err := h.mongoDB.RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
			status["mongodb"] = "error: " + err.Error()
			status["status"] = "degraded"
			httpStatus = http.StatusServiceUnavailable
		}
	}

	c.JSON(httpStatus, status)
}
