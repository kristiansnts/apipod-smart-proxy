package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	proxyConfig "github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
	"github.com/rpay/apipod-smart-proxy/internal/pool"
	"github.com/rpay/apipod-smart-proxy/internal/proxy"
)

func main() {
	logger := log.New(os.Stdout, "[apipod-smart-proxy] ", log.LstdFlags|log.Lshortfile)

	// Open runner.log (truncate on each run) for proxy request logging
	runnerFile, err := os.Create("runner.log")
	if err != nil {
		logger.Fatalf("Failed to create runner.log: %v", err)
	}
	defer runnerFile.Close()
	runnerLogger := log.New(runnerFile, "", log.LstdFlags)

	logger.Println("Starting Apipod Smart Proxy...")

	cfg, err := proxyConfig.Load()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}
	logger.Printf("Configuration loaded successfully")
	logger.Printf("Config Mode: %s", cfg.ConfigMode)
	logger.Printf("Port: %s", cfg.Port)

	// Initialize PostgreSQL (still needed for routing/quota_items)
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Println("Database initialized successfully")

	// Initialize config loader based on mode
	var configLoader proxyConfig.ConfigLoader
	switch cfg.ConfigMode {
	case "static":
		if cfg.StaticConfigPath == "" {
			logger.Fatalf("STATIC_CONFIG_PATH required for static config mode")
		}
		configLoader, err = proxyConfig.NewStaticConfigLoader(cfg.StaticConfigPath)
		if err != nil {
			logger.Fatalf("Failed to load static config: %v", err)
		}
		logger.Printf("Static config loaded from: %s", cfg.StaticConfigPath)
	case "remote":
		if cfg.BackendURL == "" || cfg.InternalAPISecret == "" {
			logger.Fatalf("BACKEND_URL and INTERNAL_API_SECRET required for remote config mode")
		}
		configLoader = proxyConfig.NewRemoteConfigLoader(cfg.BackendURL, cfg.InternalAPISecret)
		logger.Printf("Remote config loader: %s", cfg.BackendURL)
	default:
		logger.Fatalf("Invalid CONFIG_MODE: %s (expected 'remote' or 'static')", cfg.ConfigMode)
	}

	// Initialize components
	authMiddleware := middleware.NewAuthMiddleware(configLoader, runnerLogger)
	loggingMiddleware := middleware.NewLoggingMiddleware(logger)
	proxyRouter := proxy.NewRouter(db)
	modelLimiter := pool.NewModelLimiter()

	// Initialize usage committer (only for remote mode)
	var usageCommitter *proxy.UsageCommitter
	if cfg.ConfigMode == "remote" {
		usageCommitter = proxy.NewUsageCommitter(cfg.BackendURL, cfg.InternalAPISecret, runnerLogger)
	}

	proxyHandler := proxy.NewHandler(proxyRouter, db, logger, runnerLogger, modelLimiter, usageCommitter)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", proxy.HealthCheck)
	mux.Handle("/v1/chat/completions",
		loggingMiddleware.LogRequest(
			authMiddleware.Authenticate(
				http.HandlerFunc(proxyHandler.HandleChatCompletion))))
	mux.Handle("/v1/messages",
		loggingMiddleware.LogRequest(
			authMiddleware.Authenticate(
				http.HandlerFunc(proxyHandler.HandleMessages))))

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
		logger.Println("  POST /v1/chat/completions    - Chat completions (Bearer token required)")
		logger.Println("  POST /v1/messages            - Anthropic Messages API (x-api-key or Bearer token)")
		logger.Println("")
		logger.Printf("Config Mode: %s", cfg.ConfigMode)
		if cfg.ConfigMode == "static" {
			logger.Println("  Running in SELF-HOST mode (static config)")
		} else {
			logger.Println(fmt.Sprintf("  Running in SAAS mode (backend: %s)", cfg.BackendURL))
		}
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
