package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/admin"
	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
	"github.com/rpay/apipod-smart-proxy/internal/proxy"
)

func main() {
	// Create logger
	logger := log.New(os.Stdout, "[apipod-smart-proxy] ", log.LstdFlags|log.Lshortfile)

	logger.Println("Starting Apipod Smart Proxy...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}
	logger.Printf("Configuration loaded successfully")
	logger.Printf("Upstream: %s", cfg.AntigravityURL)
	logger.Printf("Database: %s", cfg.DatabasePath)
	logger.Printf("Port: %s", cfg.Port)

	// Initialize database
	db, err := database.New(cfg.DatabasePath)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Println("Database initialized successfully")

	// Initialize components
	authMiddleware := middleware.NewAuthMiddleware(db)
	loggingMiddleware := middleware.NewLoggingMiddleware(logger)
	adminHandler := admin.NewHandler(db, cfg.AdminSecret)
	proxyRouter := proxy.NewRouter()
	proxyHandler := proxy.NewHandler(cfg, proxyRouter, logger)

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Health check endpoint (no auth)
	mux.HandleFunc("/health", proxy.HealthCheck)

	// Admin endpoint (protected by admin secret)
	mux.HandleFunc("/admin/create-key", adminHandler.CreateAPIKey)

	// Proxy endpoint (protected by API key auth)
	mux.Handle("/v1/chat/completions",
		loggingMiddleware.LogRequest(
			authMiddleware.Authenticate(
				http.HandlerFunc(proxyHandler.HandleChatCompletion))))

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for streaming
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Printf("Server listening on http://0.0.0.0:%s", cfg.Port)
		logger.Println("Routes:")
		logger.Println("  GET  /health                 - Health check (no auth)")
		logger.Println("  POST /admin/create-key       - Create API key (admin secret required)")
		logger.Println("  POST /v1/chat/completions    - Chat completions (API key required)")
		logger.Println("")
		logger.Println("Smart routing active: cursor-pro-sonnet → 20% claude-sonnet-4-5, 80% gemini-3-flash")
		logger.Println("Press Ctrl+C to stop...")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutting down server...")

	// Graceful shutdown with 10 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("Server forced to shutdown: %v", err)
	}

	logger.Println("Server stopped gracefully")
}
