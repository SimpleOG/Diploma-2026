package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health handles GET /health.
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
