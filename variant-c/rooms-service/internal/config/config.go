package config

import (
	"fmt"
	"os"
)

// Config holds all configuration for the rooms-service.
type Config struct {
	ServerPort     string
	DBDSN          string
	RedisAddr      string
	AuthServiceURL string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8082"
	}

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		return nil, fmt.Errorf("AUTH_SERVICE_URL environment variable is required")
	}

	return &Config{
		ServerPort:     port,
		DBDSN:          dsn,
		RedisAddr:      redisAddr,
		AuthServiceURL: authURL,
	}, nil
}
