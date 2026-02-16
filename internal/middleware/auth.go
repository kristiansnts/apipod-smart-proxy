package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/rpay/apipod-smart-proxy/internal/database"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const userContextKey contextKey = "user"

// AuthMiddleware handles API key authentication
type AuthMiddleware struct {
	db     *database.DB
	logger *log.Logger
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(db *database.DB) *AuthMiddleware {
	return &AuthMiddleware{db: db, logger: log.Default()}
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

		// Validate API token
		valid, user, err := m.db.IsValidAPIToken(apiKey)
		if err != nil {
			m.logger.Printf("Auth error for token %s...: %v", apiKey[:min(8, len(apiKey))], err)
			http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
			return
		}

		if !valid || user == nil {
			http.Error(w, `{"error": "Invalid or expired API token"}`, http.StatusForbidden)
			return
		}

		// Store user in request context for downstream handlers
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserFromContext retrieves the authenticated user from request context
func GetUserFromContext(ctx context.Context) *database.User {
	if user, ok := ctx.Value(userContextKey).(*database.User); ok {
		return user
	}
	return nil
}
