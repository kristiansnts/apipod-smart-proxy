package keygen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateAPIKey creates a secure random API key
// Format: apk_<base64-url-safe-encoded-32-bytes>
// Result is approximately 48 characters long
func GenerateAPIKey() (string, error) {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode to base64 URL-safe format (no padding)
	encoded := base64.RawURLEncoding.EncodeToString(bytes)

	// Prefix with "apk_" for easy identification
	return "apk_" + encoded, nil
}
