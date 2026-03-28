package config

import (
	"fmt"
	"os"
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

// Load reads configuration from environment variables.
func Load() (*Config, error) {
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
