package config

import (
	"log/slog"
	"os"
	"strconv"
)

// Config holds all runtime configuration read from environment variables.
type Config struct {
	ServerPort         string
	WorkerInstances    int
	DBDSN              string
	MongoURI           string
	MongoDB            string
	KafkaBrokers       string
	KafkaTopic         string
	KafkaConsumerGroup string
	MemcachedAddr      string
	RedisAddr          string
	JWTSecret          string
	JWTExpirationHours int
}

// Load reads configuration from environment variables with sane defaults.
func Load() *Config {
	cfg := &Config{
		ServerPort:         getEnv("SERVER_PORT", "8080"),
		WorkerInstances:    getEnvInt("WORKER_INSTANCES", 4),
		DBDSN:              getEnv("DB_DSN", "postgres://chatuser:chatpass@localhost:5432/chatdb?sslmode=disable"),
		MongoURI:           getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:            getEnv("MONGO_DB", "chatdb"),
		KafkaBrokers:       getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopic:         getEnv("KAFKA_TOPIC", "chat.messages"),
		KafkaConsumerGroup: getEnv("KAFKA_CONSUMER_GROUP", "chat-workers"),
		MemcachedAddr:      getEnv("MEMCACHED_ADDR", "localhost:11211"),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		JWTExpirationHours: getEnvInt("JWT_EXPIRATION_HOURS", 24),
	}

	slog.Info("config loaded",
		"port", cfg.ServerPort,
		"worker_instances", cfg.WorkerInstances,
		"kafka_topic", cfg.KafkaTopic,
		"mongo_db", cfg.MongoDB,
	)
	return cfg
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			slog.Warn("invalid env int, using default", "key", key, "fallback", fallback)
			return fallback
		}
		return n
	}
	return fallback
}
