package auth

import (
	"encoding/json"
	"net/http"
)

type Handler struct {
	store           *DeviceStore
	verificationURL string
}

func NewHandler(store *DeviceStore, verificationURL string) *Handler {
	return &Handler{
		store:           store,
		verificationURL: verificationURL,
	}
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DeviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
}

type DeviceTokenResponse struct {
	Status   string `json:"status"`
	APIToken string `json:"api_token,omitempty"`
	Username string `json:"username,omitempty"`
	Plan     string `json:"plan,omitempty"`
	Error    string `json:"error,omitempty"`
}

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
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	dc, err := h.store.CreateCode()
	if err != nil {
		http.Error(w, `{"error": "Failed to create device code"}`, http.StatusInternalServerError)
		return
	}

	resp := DeviceCodeResponse{
		DeviceCode:      dc.DeviceCode,
		UserCode:        dc.UserCode,
		VerificationURL: h.verificationURL,
		ExpiresIn:       int(dc.ExpiresAt.Sub(dc.ExpiresAt.Add(-10 * 60e9)).Seconds()),
		Interval:        dc.Interval,
	}
	resp.ExpiresIn = 600

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleDeviceToken handles POST /auth/device/token
func (h *Handler) HandleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req DeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	dc := h.store.GetByDeviceCode(req.DeviceCode)
	if dc == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			Status: "expired",
			Error:  "Device code expired or not found",
		})
		return
	}

	if !dc.Authorized {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			Status: "pending",
		})
		return
	}

	// Authorized — return token and clean up
	resp := DeviceTokenResponse{
		Status:   "authorized",
		APIToken: dc.APIToken,
		Username: dc.Username,
		Plan:     dc.Plan,
	}

	h.store.Remove(dc.DeviceCode)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleAuthorize handles POST /auth/device/authorize (called by dashboard)
func (h *Handler) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req AuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.UserCode == "" || req.APIToken == "" {
		http.Error(w, `{"error": "user_code and api_token are required"}`, http.StatusBadRequest)
		return
	}

	dc := h.store.GetByUserCode(req.UserCode)
	if dc == nil {
		http.Error(w, `{"error": "Invalid or expired device code"}`, http.StatusNotFound)
		return
	}

	if !h.store.Authorize(req.UserCode, req.UserID, req.APIToken, req.Username, req.Plan) {
		http.Error(w, `{"error": "Failed to authorize"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "authorized"})
}

// Store returns the DeviceStore for external use
func (h *Handler) Store() *DeviceStore {
	return h.store
}
