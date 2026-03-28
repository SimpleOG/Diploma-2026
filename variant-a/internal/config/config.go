package config

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	ServerPort       string
	DBDSN            string
	RedisAddr        string
	RedisPassword    string
	JWTSecret        string
	JWTExpirationHrs int
}

// Load reads configuration from environment variables after loading a .env file.
// It is fatal if JWT_SECRET or DB_DSN is missing.
func Load() *Config {
	// Загружаем app.env, если нет — .env, если нет — берём из окружения.
	if err := godotenv.Load("app.env"); err != nil {
		if err2 := godotenv.Load(); err2 != nil {
			slog.Info("No app.env or .env file found, reading from environment")
		}
	}

	cfg := &Config{
		ServerPort:    getEnv("SERVER_PORT", "8080"),
		DBDSN:         os.Getenv("DB_DSN"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
	}

	if cfg.DBDSN == "" {
		slog.Error("DB_DSN environment variable is required")
		os.Exit(1)
	}

	if cfg.JWTSecret == "" {
		slog.Error("JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	hrs, err := strconv.Atoi(getEnv("JWT_EXPIRATION_HOURS", "24"))
	if err != nil || hrs <= 0 {
		slog.Warn("Invalid JWT_EXPIRATION_HOURS, defaulting to 24")
		hrs = 24
	}
	cfg.JWTExpirationHrs = hrs

	return cfg
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
