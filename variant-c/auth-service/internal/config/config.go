package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the auth-service.
type Config struct {
	ServerPort       string
	DBDSN            string
	JWTSecret        string
	JWTExpirationHrs int
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
		port = "8081"
	}

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	expirationHrs := 24
	if v := os.Getenv("JWT_EXPIRATION_HOURS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid JWT_EXPIRATION_HOURS: %w", err)
		}
		expirationHrs = n
	}

	return &Config{
		ServerPort:       port,
		DBDSN:            dsn,
		JWTSecret:        secret,
		JWTExpirationHrs: expirationHrs,
	}, nil
}
