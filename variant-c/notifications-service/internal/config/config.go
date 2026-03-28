package config

import (
	"fmt"
	"os"
)

// Config holds all configuration for the notifications-service.
type Config struct {
	ServerPort     string
	RabbitMQURL    string
	RedisAddr      string
	AuthServiceURL string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8084"
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL environment variable is required")
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
		RabbitMQURL:    rabbitURL,
		RedisAddr:      redisAddr,
		AuthServiceURL: authURL,
	}, nil
}
