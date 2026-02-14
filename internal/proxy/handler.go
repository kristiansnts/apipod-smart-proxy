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
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
)

// Handler handles proxy requests
type Handler struct {
	config *config.Config
	router *Router
	client *http.Client
	logger *log.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(cfg *config.Config, router *Router, logger *log.Logger) *Handler {
	return &Handler{
		config: cfg,
		router: router,
		client: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for streaming
		},
		logger: logger,
	}
}

// HandleChatCompletion handles POST /v1/chat/completions
func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
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

	// Apply smart routing
	originalModel := req.Model
	routing := h.router.RouteModel(originalModel)
	req.Model = routing.Model

	// Determine upstream URL and API key based on routing
	var upstreamURL, apiKey string
	switch routing.Upstream {
	case UpstreamGHCP:
		upstreamURL = h.config.GHCPURL + "/v1/messages"
		apiKey = h.config.GHCPKey
	case UpstreamAntigravity:
		upstreamURL = h.config.AntigravityURL + "/v1/messages"
		apiKey = h.config.AntigravityKey
	default:
		upstreamURL = h.config.AntigravityURL + "/v1/messages"
		apiKey = h.config.AntigravityKey
	}

	// Anthropic API requires max_tokens, set default if not provided
	if req.MaxTokens == nil {
		defaultMaxTokens := 4096
		req.MaxTokens = &defaultMaxTokens
	}

	// Log routing decision
	if originalModel != routing.Model || routing.Upstream != UpstreamAntigravity {
		h.logger.Printf("Smart routing: %s → %s via %s (user: %s, tier: %s)",
			originalModel, routing.Model, routing.Upstream, user.Name, user.Tier)
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

	// Copy headers from original request (except Authorization)
	for key, values := range r.Header {
		if key == "Authorization" {
			continue // Don't forward user's API key
		}
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	// Add upstream API key and required headers
	upstreamReq.Header.Set("x-api-key", apiKey)
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("anthropic-version", "2023-06-01")

	// Send request to upstream
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

	// Check if this is a streaming response
	isStreaming := req.Stream != nil && *req.Stream
	contentType := upstreamResp.Header.Get("Content-Type")

	// If upstream returns SSE, handle streaming
	if isStreaming || contentType == "text/event-stream" {
		h.handleStreamingResponse(w, upstreamResp)
	} else {
		h.handleNonStreamingResponse(w, upstreamResp)
	}
}

// handleStreamingResponse handles Server-Sent Events (SSE) streaming
func (h *Handler) handleStreamingResponse(w http.ResponseWriter, upstreamResp *http.Response) {
	// Check if ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error": "Streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Write status code
	w.WriteHeader(upstreamResp.StatusCode)

	// Create a ticker for periodic flushing
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Buffer for reading chunks
	buf := make([]byte, 4096)
	lastFlush := time.Now()

	for {
		// Read chunk from upstream
		n, err := upstreamResp.Body.Read(buf)

		if n > 0 {
			// Write chunk to client
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				h.logger.Printf("Failed to write to client: %v", writeErr)
				return
			}

			// Flush immediately if 100ms has passed since last flush
			if time.Since(lastFlush) >= 100*time.Millisecond {
				flusher.Flush()
				lastFlush = time.Now()
			}
		}

		// Check for errors
		if err == io.EOF {
			// End of stream, flush final data
			flusher.Flush()
			return
		}

		if err != nil {
			h.logger.Printf("Stream read error: %v", err)
			return
		}
	}
}

// handleNonStreamingResponse handles regular JSON responses
func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, upstreamResp *http.Response) {
	// Write status code
	w.WriteHeader(upstreamResp.StatusCode)

	// Copy response body directly
	if _, err := io.Copy(w, upstreamResp.Body); err != nil {
		h.logger.Printf("Failed to copy response: %v", err)
	}
}

// HealthCheck handles GET /health
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "healthy", "service": "apipod-smart-proxy"}`)
}
