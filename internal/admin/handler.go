package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/pkg/keygen"
)

// Handler handles admin operations
type Handler struct {
	db     *database.DB
	secret string
}

// NewHandler creates a new admin handler
func NewHandler(db *database.DB, secret string) *Handler {
	return &Handler{
		db:     db,
		secret: secret,
	}
}

// CreateKeyRequest is the request body for creating an API key
type CreateKeyRequest struct {
	Name      string `json:"name"`
	Tier      string `json:"tier,omitempty"`
	ExpiresIn *int   `json:"expires_in,omitempty"` // days
}

// CreateKeyResponse is the response for API key creation
type CreateKeyResponse struct {
	APIKey    string     `json:"api_key"`
	Name      string     `json:"name"`
	Tier      string     `json:"tier"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKey handles POST /admin/create-key
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Verify admin secret
	adminSecret := r.Header.Get("x-admin-secret")
	if adminSecret != h.secret {
		http.Error(w, `{"error": "Invalid admin secret"}`, http.StatusForbidden)
		return
	}

	// Parse request body
	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Validate name
	if req.Name == "" {
		http.Error(w, `{"error": "Name is required"}`, http.StatusBadRequest)
		return
	}

	// Set default tier
	if req.Tier == "" {
		req.Tier = "elite"
	}

	// Generate API key
	apiKey, err := keygen.GenerateAPIKey()
	if err != nil {
		http.Error(w, `{"error": "Failed to generate API key"}`, http.StatusInternalServerError)
		return
	}

	// Calculate expiration date
	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiry := time.Now().AddDate(0, 0, *req.ExpiresIn)
		expiresAt = &expiry
	}

	// Create user in database
	user, err := h.db.CreateUser(req.Name, apiKey, req.Tier, expiresAt)
	if err != nil {
		http.Error(w, `{"error": "Failed to create user"}`, http.StatusInternalServerError)
		return
	}

	// Return response
	resp := CreateKeyResponse{
		APIKey:    user.APIKey,
		Name:      user.Name,
		Tier:      user.Tier,
		CreatedAt: user.CreatedAt,
		ExpiresAt: user.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}
