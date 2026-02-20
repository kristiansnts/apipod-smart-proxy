package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string

	// Config mode: "remote" (SaaS) or "static" (OSS self-host)
	ConfigMode string

	// Remote mode: Laravel backend URL + shared secret
	BackendURL        string
	InternalAPISecret string

	// Static mode: path to config.json
	StaticConfigPath string
}

func Load() (*Config, error) {
	_ = godotenv.Load() // silently ignore if .env doesn't exist

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}

	configMode := os.Getenv("CONFIG_MODE")
	if configMode == "" {
		configMode = "remote" // default to SaaS mode
	}

	return &Config{
		Port:              port,
		DatabaseURL:       databaseURL,
		ConfigMode:        configMode,
		BackendURL:        os.Getenv("BACKEND_URL"),
		InternalAPISecret: os.Getenv("INTERNAL_API_SECRET"),
		StaticConfigPath:  os.Getenv("STATIC_CONFIG_PATH"),
	}, nil
}
