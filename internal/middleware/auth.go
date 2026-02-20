package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/rpay/apipod-smart-proxy/internal/config"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const runtimeConfigKey contextKey = "runtime_config"

// AuthMiddleware handles API key authentication via ConfigLoader.
// The proxy is stateless — auth is delegated to the config loader
// (either remote API or static file).
type AuthMiddleware struct {
	configLoader config.ConfigLoader
	logger       *log.Logger
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(configLoader config.ConfigLoader, logger *log.Logger) *AuthMiddleware {
	return &AuthMiddleware{configLoader: configLoader, logger: logger}
}

// Authenticate wraps an HTTP handler with API key authentication.
// Supports both "Authorization: Bearer <token>" and "x-api-key: <token>" headers.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract API key from Authorization header or x-api-key header
		var apiKey string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				http.Error(w, `{"error": "Invalid Authorization header format. Expected: Bearer <token>"}`, http.StatusUnauthorized)
				return
			}
			apiKey = strings.TrimPrefix(authHeader, bearerPrefix)
		} else {
			apiKey = r.Header.Get("x-api-key")
		}

		if apiKey == "" {
			http.Error(w, `{"error": "Missing API key. Provide Authorization: Bearer <token> or x-api-key header"}`, http.StatusUnauthorized)
			return
		}

		// Get runtime config from config loader (remote API or static file)
		cfg, err := m.configLoader.GetRuntimeConfig(apiKey)
		if err != nil {
			m.logger.Printf("Auth error: %v", err)
			http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
			return
		}

		if cfg == nil || !cfg.Allowed {
			reason := "Invalid or expired API token"
			if cfg != nil && cfg.Reason != "" {
				reason = cfg.Reason
			}

			// Map reason to status code
			statusCode := http.StatusForbidden
			if strings.Contains(reason, "Invalid") || strings.Contains(reason, "revoked") {
				statusCode = http.StatusUnauthorized
			} else if strings.Contains(reason, "exceeded") || strings.Contains(reason, "limit") {
				statusCode = http.StatusTooManyRequests
			}

			m.logger.Printf("AUTH_REJECTED [%d] reason=%q key=%s", statusCode, reason, maskAPIKey(apiKey))
			http.Error(w, `{"error": "`+reason+`"}`, statusCode)
			return
		}

		// Store runtime config in request context for downstream handlers
		ctx := context.WithValue(r.Context(), runtimeConfigKey, cfg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetConfigFromContext retrieves the runtime config from request context.
func GetConfigFromContext(ctx context.Context) *config.RuntimeConfig {
	if cfg, ok := ctx.Value(runtimeConfigKey).(*config.RuntimeConfig); ok {
		return cfg
	}
	return nil
}

// maskAPIKey returns a redacted version of an API key for safe logging.
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
