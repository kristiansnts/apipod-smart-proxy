package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/antigravity"
)

var (
	antigravityPools   = make(map[uint]*antigravity.AccountPool)
	antigravityPoolsMu sync.RWMutex
)

func (h *Handler) getAntigravityPool(providerID uint) (*antigravity.AccountPool, error) {
	antigravityPoolsMu.RLock()
	pool, exists := antigravityPools[providerID]
	antigravityPoolsMu.RUnlock()

	if exists {
		return pool, nil
	}

	antigravityPoolsMu.Lock()
	defer antigravityPoolsMu.Unlock()

	// Double check
	if pool, exists := antigravityPools[providerID]; exists {
		return pool, nil
	}

	// Load from DB
	dbAccounts, err := h.db.GetActiveAccountsForProvider(providerID)
	if err != nil {
		return nil, err
	}

	newPool := antigravity.NewAccountPool(3 * time.Second) // 3s cooldown
	for _, dbAcc := range dbAccounts {
		newPool.Accounts = append(newPool.Accounts, &antigravity.Account{
			ID:           dbAcc.ID,
			Email:        dbAcc.Email,
			RefreshToken: dbAcc.APIKey,
		})
	}

	antigravityPools[providerID] = newPool
	return newPool, nil
}

// Handler handles proxy requests
type Handler struct {
	config *config.Config
	router *Router
	db     *database.DB
	client *http.Client
	logger *log.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(cfg *config.Config, router *Router, db *database.DB, logger *log.Logger) *Handler {
	return &Handler{
		config: cfg,
		router: router,
		db:     db,
		client: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for streaming
		},
		logger: logger,
	}
}

// HandleChatCompletion handles POST /v1/chat/completions
func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Read and parse request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error": "Failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, `{"error": "Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Apply DB-driven smart routing based on user's subscription
	routing, err := h.router.RouteModel(user.SubID, req.Model)
	if err != nil {
		h.logger.Printf("Routing error for sub_id=%d: %v", user.SubID, err)
		http.Error(w, `{"error": "Routing failed or no models configured"}`, http.StatusInternalServerError)
		return
	}
	originalModel := req.Model
	req.Model = routing.Model

	// Determine upstream URL and API key dynamically from DB
	var upstreamURL string
	var isNative bool
	
	switch routing.ProviderType {
	case "openai":
		upstreamURL = routing.BaseURL + "/v1/chat/completions"
	case "antigravity_native":
		isNative = true
		// Logic will be handled below
	case "copilot_native":
		isNative = true
		// Logic will be handled below
	default:
		// Default to Anthropic format
		upstreamURL = routing.BaseURL + "/v1/messages"
	}
	
	apiKey := routing.APIKey

	h.logger.Printf("Routing: %s -> %s via %s (Type: %s, Native: %v)",
		originalModel, routing.Model, routing.BaseURL, routing.ProviderType, isNative)

	if isNative {
		h.handleNativeUpstream(w, r, routing, user, originalModel, bodyBytes)
		return
	}

	// Re-encode modified request
	modifiedBody, err := json.Marshal(req)
	if err != nil {
		http.Error(w, `{"error": "Failed to encode request"}`, http.StatusInternalServerError)
		return
	}

	// Create upstream request
	upstreamReq, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(modifiedBody))
	if err != nil {
		http.Error(w, `{"error": "Failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Copy headers (except Authorization and restricted ones)
	for key, values := range r.Header {
		if key == "Authorization" || key == "Content-Type" || key == "x-api-key" {
			continue
		}
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	upstreamReq.Header.Set("x-api-key", apiKey)
	upstreamReq.Header.Set("Content-Type", "application/json")

	// Add provider-specific headers
	if routing.ProviderType == "anthropic" {
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Send to upstream
	upstreamResp, err := h.client.Do(upstreamReq)
	if err != nil {
		h.logger.Printf("Upstream request failed: %v", err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// Copy response headers
	for key, values := range upstreamResp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	isStreaming := req.Stream != nil && *req.Stream
	contentType := upstreamResp.Header.Get("Content-Type")

	usageCtx := database.UsageContext{
		QuotaItemID:      routing.QuotaItemID,
		UserID:           user.ID,
		RequestedModel:   originalModel,
		RoutedModel:      routing.Model,
		UpstreamProvider: routing.BaseURL,
	}

	if isStreaming || contentType == "text/event-stream" {
		h.handleStreamingResponse(w, upstreamResp, usageCtx)
	} else {
		h.handleNonStreamingResponse(w, upstreamResp, usageCtx)
	}
}

// handleStreamingResponse handles Server-Sent Events (SSE) streaming
func (h *Handler) handleStreamingResponse(w http.ResponseWriter, upstreamResp *http.Response, usageCtx database.UsageContext) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error": "Streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(upstreamResp.StatusCode)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 4096)
	lastFlush := time.Now()

	for {
		n, err := upstreamResp.Body.Read(buf)

		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				h.logger.Printf("Failed to write to client: %v", writeErr)
				return
			}
			if time.Since(lastFlush) >= 100*time.Millisecond {
				flusher.Flush()
				lastFlush = time.Now()
			}
		}

		if err == io.EOF {
			flusher.Flush()
			// Log usage with 0 tokens for streaming (token count unavailable)
			if usageCtx.QuotaItemID > 0 {
				if logErr := h.db.LogUsage(usageCtx, 0, 0); logErr != nil {
					h.logger.Printf("Failed to log usage: %v", logErr)
				}
			}
			return
		}

		if err != nil {
			h.logger.Printf("Stream read error: %v", err)
			return
		}
	}
}

// handleNonStreamingResponse handles regular JSON responses and logs token usage
func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, upstreamResp *http.Response, usageCtx database.UsageContext) {
	w.WriteHeader(upstreamResp.StatusCode)

	respBytes, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		h.logger.Printf("Failed to read response: %v", err)
		return
	}

	if _, err := w.Write(respBytes); err != nil {
		h.logger.Printf("Failed to write response: %v", err)
	}

	// Extract token count and log usage
	if usageCtx.QuotaItemID > 0 && upstreamResp.StatusCode == http.StatusOK {
		inputTokens, outputTokens := extractTokenCounts(respBytes)
		if logErr := h.db.LogUsage(usageCtx, inputTokens, outputTokens); logErr != nil {
			h.logger.Printf("Failed to log usage: %v", logErr)
		}
	}
}

// extractTokenCounts parses input_tokens + output_tokens from an Anthropic or OpenAI response
func extractTokenCounts(body []byte) (int, int) {
	// Try OpenAI format first
	var oaResp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &oaResp); err == nil && oaResp.Usage.PromptTokens > 0 {
		return oaResp.Usage.PromptTokens, oaResp.Usage.CompletionTokens
	}

	// Try Anthropic format
	var antResp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &antResp); err == nil {
		return antResp.Usage.InputTokens, antResp.Usage.OutputTokens
	}

	return 0, 0
}

// HealthCheck handles GET /health
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "healthy", "service": "apipod-smart-proxy"}`)
}
