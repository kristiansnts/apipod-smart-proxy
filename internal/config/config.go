package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port         string
	DatabaseURL  string
	AdminSecret  string
	DashboardURL string
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

	dashboardURL := os.Getenv("DASHBOARD_URL")
	if dashboardURL == "" {
		dashboardURL = "http://localhost:8000"
	}

	return &Config{
		Port:         port,
		DatabaseURL:  databaseURL,
		AdminSecret:  adminSecret,
		DashboardURL: dashboardURL,
	}, nil
}
