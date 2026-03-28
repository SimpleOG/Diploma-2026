package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// HealthHandler handles health check requests.
type HealthHandler struct {
	mongoClient *mongo.Client
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(mongoClient *mongo.Client) *HealthHandler {
	return &HealthHandler{mongoClient: mongoClient}
}

// Health handles GET /health.
func (h *HealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	if err := h.mongoClient.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"mongo":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
