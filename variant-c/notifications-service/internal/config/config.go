package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the notifications-service.
type Config struct {
	ServerPort     string
	RabbitMQURL    string
	RedisAddr      string
	AuthServiceURL string
}

// Load reads configuration from app.env (или .env, или переменные окружения).
func Load() (*Config, error) {
	// Загружаем app.env, если нет — .env, если нет — берём из окружения.
	if err := godotenv.Load("app.env"); err != nil {
		if err2 := godotenv.Load(); err2 != nil {
			slog.Info("No app.env or .env file found, reading from environment")
		}
	}

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
