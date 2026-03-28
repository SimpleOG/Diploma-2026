package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the messages-service.
type Config struct {
	ServerPort      string
	MongoURI        string
	MongoDB         string
	RabbitMQURL     string
	AuthServiceURL  string
	RoomsServiceURL string
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
		port = "8083"
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI environment variable is required")
	}

	mongoDB := os.Getenv("MONGO_DB")
	if mongoDB == "" {
		mongoDB = "chatdb"
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL environment variable is required")
	}

	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		return nil, fmt.Errorf("AUTH_SERVICE_URL environment variable is required")
	}

	roomsURL := os.Getenv("ROOMS_SERVICE_URL")
	if roomsURL == "" {
		return nil, fmt.Errorf("ROOMS_SERVICE_URL environment variable is required")
	}

	return &Config{
		ServerPort:      port,
		MongoURI:        mongoURI,
		MongoDB:         mongoDB,
		RabbitMQURL:     rabbitURL,
		AuthServiceURL:  authURL,
		RoomsServiceURL: roomsURL,
	}, nil
}
