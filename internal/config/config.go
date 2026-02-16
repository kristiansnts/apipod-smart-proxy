package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	AdminSecret string
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

	adminSecret := os.Getenv("ADMIN_SECRET")
	if adminSecret == "" {
		return nil, fmt.Errorf("ADMIN_SECRET not set")
	}

	return &Config{
		Port:        port,
		DatabaseURL: databaseURL,
		AdminSecret: adminSecret,
	}, nil
}
