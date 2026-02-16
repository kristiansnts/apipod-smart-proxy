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
	logger := log.New(os.Stdout, "[apipod-smart-proxy] ", log.LstdFlags|log.Lshortfile)
	logger.Println("Starting Apipod Smart Proxy...")

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}
	logger.Printf("Configuration loaded successfully")
	// logger.Printf("Upstream Antigravity: %s", cfg.AntigravityURL) // Removed, now local to Rust Engine
	logger.Printf("Database: %s", cfg.DatabaseURL)
	logger.Printf("Port: %s", cfg.Port)

	// Initialize PostgreSQL
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Println("Database initialized successfully")

	// Seed subscription plans on startup
	if err := db.Seed(); err != nil {
		logger.Fatalf("Failed to seed database: %v", err)
	}
	logger.Println("Subscription plans seeded")

	// Initialize components
	authMiddleware := middleware.NewAuthMiddleware(db)
	loggingMiddleware := middleware.NewLoggingMiddleware(logger)
	adminHandler := admin.NewHandler(db, cfg.AdminSecret)
	proxyRouter := proxy.NewRouter(db)
	
	// Pass cfg to NewHandler
	proxyHandler := proxy.NewHandler(cfg, proxyRouter, db, logger)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", proxy.HealthCheck)
	mux.HandleFunc("/admin/create-key", adminHandler.CreateAPIKey)
	mux.Handle("/v1/chat/completions",
		loggingMiddleware.LogRequest(
			authMiddleware.Authenticate(
				http.HandlerFunc(proxyHandler.HandleChatCompletion))))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Printf("Server listening on http://0.0.0.0:%s", cfg.Port)
		logger.Println("Routes:")
		logger.Println("  GET  /health                 - Health check")
		logger.Println("  POST /admin/create-key       - Create API token (x-admin-secret required)")
		logger.Println("  POST /v1/chat/completions    - Chat completions (Bearer token required)")
		logger.Println("")
		logger.Println("Subscription plans: cursor-pro-auto | cursor-pro-sonnet | cursor-pro-opus")
		logger.Println("Press Ctrl+C to stop...")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("Server forced to shutdown: %v", err)
	}
	logger.Println("Server stopped gracefully")
}
