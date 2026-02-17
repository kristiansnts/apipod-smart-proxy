package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/antigravity"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/anthropiccompat"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/copilot"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/openaicompat"
)

func (h *Handler) handleNativeUpstream(w http.ResponseWriter, r *http.Request, routing RoutingResult, user *database.User, originalModel string, bodyBytes []byte) {
	usageCtx := database.UsageContext{
		QuotaItemID:      routing.QuotaItemID,
		UserID:           user.ID,
		RequestedModel:   originalModel,
		RoutedModel:      routing.Model,
		UpstreamProvider: "native:" + routing.ProviderType,
	}
	startTime := time.Now()
	username := user.Username

	switch routing.ProviderType {
	case "antigravity_proxy":
		h.handleAntigravityNative(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "cliproxy":
		h.handleCopilotNative(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "groq", "openai":
		h.handleOpenAICompat(w, r, routing, usageCtx, bodyBytes, startTime, username)
	default:
		http.Error(w, `{"error": "Unsupported provider type"}`, http.StatusNotImplemented)
	}
}

// resolveAPIKey checks the account pool for the provider and returns a pooled key if available,
// otherwise falls back to the provider's default API key.
func (h *Handler) resolveAPIKey(routing RoutingResult) string {
	if p := h.getPool(routing.ProviderID); p != nil {
		acc := p.GetReadyAccount()
		if acc != nil {
			h.logger.Printf("[%s] using pooled account %s (id=%d)", routing.ProviderType, acc.Email, acc.ID)
			return acc.APIKey
		}
		h.logger.Printf("[%s] all pooled accounts rate-limited, falling back to provider key (len=%d)", routing.ProviderType, len(routing.APIKey))
	}
	return routing.APIKey
}

func (h *Handler) handleOpenAICompat(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	// Replace model name in request body with the routed model
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		h.logger.Printf("[%s] JSON parse error: %v (body length=%d)", routing.ProviderType, err, len(bodyBytes))
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return
	}
	
	isStream := false
	if stream, ok := body["stream"].(bool); ok && stream {
		isStream = true
	}
	
	body["model"] = routing.Model
	bodyBytes, _ = json.Marshal(body)

	path := "/v1/chat/completions"
	if routing.ProviderType == "groq" {
		path = "/openai/v1/chat/completions"
	}

	apiKey := h.resolveAPIKey(routing)

	upstreamURL := routing.BaseURL + path
	keyHint := ""
	if len(apiKey) > 8 {
		keyHint = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	}
	resp, err := openaicompat.Proxy(routing.BaseURL, apiKey, path, bodyBytes)
	if err != nil {
		h.runnerLogger.Printf("ERROR [%s] model=%s url=%s key=%s user=%s latency=%s err=%v", routing.ProviderType, routing.Model, upstreamURL, keyHint, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	// Handle error responses - always buffer to log
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [%s] status=%d model=%s url=%s key=%s user=%s latency=%s body=%s", routing.ProviderType, resp.StatusCode, routing.Model, upstreamURL, keyHint, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Copy headers
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	var inputTokens, outputTokens int
	hasToolCall := false

	// Stream response directly while capturing tokens
	if isStream {
		inputTokens, outputTokens, hasToolCall = openaicompat.StreamTransform(resp.Body, w)
	} else {
		// Non-streaming: buffer to parse usage
		respBody, _ := io.ReadAll(resp.Body)
		w.Write(respBody)

		// Extract token usage from response
		inputTokens, outputTokens, _ = openaicompat.ExtractTokens(respBody)
		hasToolCall = openaicompat.DetectToolCall(respBody)
	}

	h.runnerLogger.Printf("OK [%s] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.ProviderType, routing.Model, isStream, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
}

func (h *Handler) handleCopilotNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	resp, upstreamURL, err := copilot.ProxyToCopilot(routing.BaseURL, routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		keyPrefix := "unknown"
		if len(routing.APIKey) > 6 {
			keyPrefix = routing.APIKey[:4] + "..." + routing.APIKey[len(routing.APIKey)-3:]
		}
		h.runnerLogger.Printf("ERROR [cliproxy] model=%s url=%s key=%s user=%s latency=%s err=%v", routing.Model, upstreamURL, keyPrefix, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		keyPrefix := "unknown"
		if len(routing.APIKey) > 6 {
			keyPrefix = routing.APIKey[:4] + "..." + routing.APIKey[len(routing.APIKey)-3:]
		}
		h.runnerLogger.Printf("ERROR [cliproxy] status=%d model=%s url=%s key=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, upstreamURL, keyPrefix, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	} else {
		h.runnerLogger.Printf("OK [cliproxy] model=%s stream=%v latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func (h *Handler) handleAntigravityNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	apiKey := h.resolveAPIKey(routing)

	resp, err := antigravity.ProxyToAntigravity(routing.BaseURL, apiKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [antigravity_proxy] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [antigravity_proxy] status=%d model=%s url=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, 0, 0)
		}
		return
	}

	if req.Stream {
		// Convert Anthropic SSE stream back to OpenAI SSE format for the client
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		in, out, hasToolCall := antigravity.StreamTransformToOpenAI(resp.Body, w, routing.Model)
		h.runnerLogger.Printf("OK [antigravity_proxy] model=%s stream=true tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	} else {
		// Convert Anthropic response back to OpenAI format for the client
		respBytes, _ := io.ReadAll(resp.Body)
		transformed, in, out, hasToolCall, err := antigravity.TransformResponseToOpenAI(respBytes, routing.Model)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBytes)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(transformed)
		h.runnerLogger.Printf("OK [antigravity_proxy] model=%s stream=false tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	}
}

// detectAnthropicToolCall checks if an Anthropic Messages response contains tool use.
func detectAnthropicToolCall(body []byte) bool {
	var resp struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return false
	}
	if resp.StopReason == "tool_use" {
		return true
	}
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

// --- Anthropic Messages API endpoint handlers ---

func (h *Handler) handleNativeUpstreamAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, user *database.User, originalModel string, bodyBytes []byte) {
	usageCtx := database.UsageContext{
		QuotaItemID:      routing.QuotaItemID,
		UserID:           user.ID,
		RequestedModel:   originalModel,
		RoutedModel:      routing.Model,
		UpstreamProvider: "anthropic:" + routing.ProviderType,
	}
	startTime := time.Now()
	username := user.Username

	switch routing.ProviderType {
	case "antigravity_proxy":
		h.handleAntigravityFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "cliproxy":
		h.handleCopilotFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "groq", "openai":
		h.handleOpenAICompatFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	default:
		http.Error(w, `{"error": {"type": "not_found_error", "message": "Unsupported provider type"}}`, http.StatusNotImplemented)
	}
}

// handleAntigravityFromAnthropic proxies an Anthropic-format request directly to an Anthropic-compatible upstream.
func (h *Handler) handleAntigravityFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	// For Claude Code requests, only replace model and pass through untouched
	// For other requests, inject system message and tools
	bodyBytes = anthropiccompat.InjectSystemMessage(bodyBytes, routing.Model)

	apiKey := h.resolveAPIKey(routing)

	resp, err := anthropiccompat.ProxyDirect(routing.BaseURL, apiKey, bodyBytes)
	if err != nil {
		h.runnerLogger.Printf("ERROR [antigravity_proxy/anthropic] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [antigravity_proxy/anthropic] status=%d model=%s url=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, 0, 0)
		}
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		in, out := antigravity.StreamTransform(resp.Body, w)
		h.runnerLogger.Printf("OK [antigravity_proxy/anthropic] model=%s stream=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	} else {
		respBytes, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBytes)

		// Detect tool calls from Anthropic response
		hasToolCall := detectAnthropicToolCall(respBytes)

		// Extract tokens from Anthropic response
		var anthropicResp struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		in, out := 0, 0
		if json.Unmarshal(respBytes, &anthropicResp) == nil {
			in, out = anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens
		}
		h.runnerLogger.Printf("OK [antigravity_proxy/anthropic] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	}
}

// handleCopilotFromAnthropic proxies an Anthropic-format request to a Copilot proxy (already speaks Anthropic format).
func (h *Handler) handleCopilotFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	// Replace model with routed model and inject system message
	bodyBytes = anthropiccompat.InjectSystemMessage(bodyBytes, routing.Model)

	// Deduplicate tool_result blocks with the same tool_use_id
	// The upstream OpenAI-compatible endpoint rejects duplicate tool_call_id values
	bodyBytes = anthropiccompat.DeduplicateToolResults(bodyBytes)

	resp, upstreamURL, err := copilot.ProxyToCopilot(routing.BaseURL, routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [cliproxy/anthropic] model=%s url=%s user=%s latency=%s err=%v", routing.Model, upstreamURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [cliproxy/anthropic] status=%d model=%s url=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, upstreamURL, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	} else {
		h.runnerLogger.Printf("OK [cliproxy/anthropic] model=%s stream=%v latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

// handleOpenAICompatFromAnthropic converts Anthropic→OpenAI, proxies, then converts response back to Anthropic.
func (h *Handler) handleOpenAICompatFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) {
	// Convert Anthropic request to OpenAI format
	openaiBody, isStream, err := anthropiccompat.AnthropicToOpenAI(bodyBytes)
	if err != nil {
		h.logger.Printf("[%s/anthropic] conversion error: %v", routing.ProviderType, err)
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Invalid request body"}}`, http.StatusBadRequest)
		return
	}

	// Replace model with routed model
	var body map[string]interface{}
	json.Unmarshal(openaiBody, &body)
	body["model"] = routing.Model
	openaiBody, _ = json.Marshal(body)

	path := "/v1/chat/completions"
	if routing.ProviderType == "groq" {
		path = "/openai/v1/chat/completions"
	}

	apiKey := h.resolveAPIKey(routing)

	upstreamURL := routing.BaseURL + path
	keyHint := ""
	if len(apiKey) > 8 {
		keyHint = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	}

	resp, err := openaicompat.Proxy(routing.BaseURL, apiKey, path, openaiBody)
	if err != nil {
		h.runnerLogger.Printf("ERROR [%s/anthropic] model=%s url=%s key=%s user=%s latency=%s err=%v", routing.ProviderType, routing.Model, upstreamURL, keyHint, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [%s/anthropic] status=%d model=%s url=%s key=%s user=%s latency=%s body=%s", routing.ProviderType, resp.StatusCode, routing.Model, upstreamURL, keyHint, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	var inputTokens, outputTokens int
	hasToolCall := false

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		inputTokens, outputTokens, hasToolCall = anthropiccompat.OpenAIStreamToAnthropicStream(resp.Body, w, routing.Model)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		anthropicResp, in, out, tc, err := anthropiccompat.OpenAIResponseToAnthropic(respBody, routing.Model)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBody)
			return
		}
		inputTokens, outputTokens, hasToolCall = in, out, tc
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(anthropicResp)
	}

	h.runnerLogger.Printf("OK [%s/anthropic] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.ProviderType, routing.Model, isStream, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
}
