package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the auth-service.
type Config struct {
	ServerPort       string
	DBDSN            string
	JWTSecret        string
	JWTExpirationHrs int
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
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

	expirationHrsStr := os.Getenv("JWT_EXPIRATION_HOURS")
	expirationHrs := 24
	if expirationHrsStr != "" {
		var err error
		expirationHrs, err = strconv.Atoi(expirationHrsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid JWT_EXPIRATION_HOURS: %w", err)
		}
	}

	return &Config{
		ServerPort:       port,
		DBDSN:            dsn,
		JWTSecret:        secret,
		JWTExpirationHrs: expirationHrs,
	}, nil
}
