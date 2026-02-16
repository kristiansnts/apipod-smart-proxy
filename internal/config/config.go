package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                     string
	DatabaseURL              string
	AdminSecret              string
	AntigravityURL           string // This is now deprecated, Rust Engine is on localhost:8045
	AntigravityInternalAPIKey string // New: API key for the Rust Antigravity Manager
}

func Load() (*Config, error) {
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

	// Previously used for Node.js engine, now Rust is on localhost:8045
	// antigravityURL := os.Getenv("ANTIGRAVITY_URL")
	// if antigravityURL == "" {
	// 	antigravityURL = "http://localhost:8080" // Default for local Node.js Antigravity
	// }

	antigravityInternalAPIKey := os.Getenv("ANTIGRAVITY_INTERNAL_API_KEY")
	if antigravityInternalAPIKey == "" {
		return nil, fmt.Errorf("ANTIGRAVITY_INTERNAL_API_KEY not set")
	}


	return &Config{
		Port:                      port,
		DatabaseURL:               databaseURL,
		AdminSecret:               adminSecret,
		// AntigravityURL:            antigravityURL,
		AntigravityInternalAPIKey: antigravityInternalAPIKey,
	}, nil
}
