package deviceauth

import (
	"encoding/json"
	"net/http"
	"time"
)

const defaultTTL = 10 * time.Minute
const defaultInterval = 5 // seconds

// Handler handles device authorization HTTP endpoints
type Handler struct {
	store           *Store
	verificationURL string
}

// NewHandler creates a new device auth handler
func NewHandler(store *Store, dashboardURL string) *Handler {
	return &Handler{
		store:           store,
		verificationURL: dashboardURL + "/auth/device",
	}
}

// DeviceCodeResponse is returned by POST /auth/device/code
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

// TokenRequest is the request body for POST /auth/device/token
type TokenRequest struct {
	DeviceCode string `json:"device_code"`
}

// TokenResponse is returned by POST /auth/device/token
type TokenResponse struct {
	Status   string `json:"status"`
	APIToken string `json:"api_token,omitempty"`
	Username string `json:"username,omitempty"`
	Plan     string `json:"plan,omitempty"`
}

// AuthorizeRequest is the request body from the dashboard POST /auth/device/authorize
type AuthorizeRequest struct {
	UserCode string `json:"user_code"`
	UserID   int    `json:"user_id"`
	APIToken string `json:"api_token"`
	Username string `json:"username"`
	Plan     string `json:"plan"`
}

// HandleDeviceCode handles POST /auth/device/code
func (h *Handler) HandleDeviceCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	req := h.store.CreateRequest(defaultTTL)

	resp := DeviceCodeResponse{
		DeviceCode:      req.DeviceCode,
		UserCode:        req.UserCode,
		VerificationURL: h.verificationURL,
		Interval:        defaultInterval,
		ExpiresIn:       int(defaultTTL.Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleDeviceToken handles POST /auth/device/token
func (h *Handler) HandleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.DeviceCode == "" {
		http.Error(w, `{"error":"device_code is required"}`, http.StatusBadRequest)
		return
	}

	device := h.store.GetByDeviceCode(req.DeviceCode)
	if device == nil {
		http.Error(w, `{"error":"invalid device code"}`, http.StatusNotFound)
		return
	}

	resp := TokenResponse{Status: device.Status}
	if device.Status == "authorized" {
		resp.APIToken = device.APIToken
		resp.Username = device.Username
		resp.Plan = device.Plan
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleAuthorize handles POST /auth/device/authorize (called by dashboard)
func (h *Handler) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req AuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.UserCode == "" || req.APIToken == "" {
		http.Error(w, `{"error":"user_code and api_token are required"}`, http.StatusBadRequest)
		return
	}

	if err := h.store.Authorize(req.UserCode, req.APIToken, req.Username, req.Plan); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "authorized"})
}
