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
	return &Handler{db: db, secret: secret}
}

// CreateKeyRequest is the request body for creating an API token
type CreateKeyRequest struct {
	Name      string `json:"name"`
	SubName   string `json:"sub_name"`            // subscription plan name
	ExpiresIn *int   `json:"expires_in,omitempty"` // days
}

// CreateKeyResponse is the response for API token creation
type CreateKeyResponse struct {
	APIToken  string     `json:"api_token"`
	Name      string     `json:"name"`
	SubName   string     `json:"sub_name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKey handles POST /admin/create-key
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("x-admin-secret") != h.secret {
		http.Error(w, `{"error": "Invalid admin secret"}`, http.StatusForbidden)
		return
	}

	var req CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, `{"error": "name is required"}`, http.StatusBadRequest)
		return
	}
	if req.SubName == "" {
		http.Error(w, `{"error": "sub_name is required (e.g. cursor-pro-sonnet)"}`, http.StatusBadRequest)
		return
	}

	// Resolve subscription
	sub, err := h.db.GetSubscriptionByName(req.SubName)
	if err != nil {
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}
	if sub == nil {
		http.Error(w, `{"error": "Unknown subscription plan"}`, http.StatusBadRequest)
		return
	}

	// Generate API token
	apiToken, err := keygen.GenerateAPIKey()
	if err != nil {
		http.Error(w, `{"error": "Failed to generate API token"}`, http.StatusInternalServerError)
		return
	}

	// Calculate expiration
	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiry := time.Now().AddDate(0, 0, *req.ExpiresIn)
		expiresAt = &expiry
	}

	// Create user
	user, err := h.db.CreateUser(req.Name, apiToken, sub.SubID, expiresAt)
	if err != nil {
		http.Error(w, `{"error": "Failed to create user"}`, http.StatusInternalServerError)
		return
	}

	resp := CreateKeyResponse{
		APIToken:  user.APIToken,
		Name:      user.Username,
		SubName:   sub.SubName,
		CreatedAt: user.CreatedAt,
		ExpiresAt: user.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}
