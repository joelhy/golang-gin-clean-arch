package config

import (
	"os"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	DB struct {
		Host     string
		Port     int
		User     string
		Password string
		Name     string
	}
	Server struct {
		Port string
		Mode string
	}
	JWT struct {
		Secret string
	}
}

// NewConfig creates a new configuration instance with values from environment variables
func NewConfig() *Config {
	cfg := &Config{}

	// Database configuration
	cfg.DB.Host = getEnv("DB_HOST", "localhost")
	cfg.DB.Port = getEnvAsInt("DB_PORT", 3306)
	cfg.DB.User = getEnv("DB_USER", "root")
	cfg.DB.Password = getEnv("DB_PASSWORD", "password")
	cfg.DB.Name = getEnv("DB_NAME", "clean_arch_db")

	// Server configuration
	cfg.Server.Port = getEnv("SERVER_PORT", "8080")
	cfg.Server.Mode = getEnv("GIN_MODE", "debug")

	// JWT configuration
	cfg.JWT.Secret = getEnv("JWT_SECRET", "default-secret-key")

	return cfg
}

// getEnv gets an environment variable with a default fallback
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as integer with a default fallback
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
