package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/rpay/apipod-smart-proxy/internal/database"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const userContextKey contextKey = "user"

// AuthMiddleware handles API key authentication
type AuthMiddleware struct {
	db *database.DB
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(db *database.DB) *AuthMiddleware {
	return &AuthMiddleware{db: db}
}

// Authenticate wraps an HTTP handler with API key authentication
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error": "Missing Authorization header"}`, http.StatusUnauthorized)
			return
		}

		// Extract Bearer token
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			http.Error(w, `{"error": "Invalid Authorization header format. Expected: Bearer <token>"}`, http.StatusUnauthorized)
			return
		}

		apiKey := strings.TrimPrefix(authHeader, bearerPrefix)
		if apiKey == "" {
			http.Error(w, `{"error": "Empty API key"}`, http.StatusUnauthorized)
			return
		}

		// Validate API token
		valid, user, err := m.db.IsValidAPIToken(apiKey)
		if err != nil {
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
