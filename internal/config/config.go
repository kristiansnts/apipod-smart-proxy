package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	AntigravityURL string
	AntigravityKey string
	GHCPURL        string
	GHCPKey        string
	AdminSecret    string
	Port           string
	DatabaseURL    string
}

// Load reads configuration from environment variables
// It attempts to load from .env file first, then falls back to system environment
func Load() (*Config, error) {
	// Try to load .env file (ignore error if file doesn't exist)
	_ = godotenv.Load()

	cfg := &Config{
		AntigravityURL: getEnv("ANTIGRAVITY_URL", "http://agy.abdiku.app"),
		AntigravityKey: os.Getenv("ANTIGRAVITY_KEY"),
		GHCPURL:        getEnv("GHCP_URL", "http://127.0.0.1:8317/api/provider/ghcp"),
		GHCPKey:        getEnv("GHCP_KEY", "ccs-internal-managed"),
		AdminSecret: os.Getenv("ADMIN_SECRET"),
		Port:        getEnv("PORT", "8081"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://local:local@127.0.0.1/apipod?sslmode=disable"),
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that all required configuration is present
func (c *Config) Validate() error {
	if c.AntigravityKey == "" {
		return fmt.Errorf("ANTIGRAVITY_KEY is required but not set")
	}
	if c.GHCPKey == "" {
		return fmt.Errorf("GHCP_KEY is required but not set")
	}
	if c.AdminSecret == "" {
		return fmt.Errorf("ADMIN_SECRET is required but not set")
	}
	if len(c.AdminSecret) < 16 {
		return fmt.Errorf("ADMIN_SECRET must be at least 16 characters for security")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required but not set")
	}
	return nil
}

// getEnv returns environment variable value or default if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
