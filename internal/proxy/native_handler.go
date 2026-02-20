package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/orchestrator"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/antigravity"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/anthropiccompat"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/copilot"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/googleaistudio"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/openaicompat"
)

func (h *Handler) handleNativeUpstream(w http.ResponseWriter, r *http.Request, routing RoutingResult, user *database.User, originalModel string, bodyBytes []byte) (int, int, bool) {
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
		return h.handleAntigravityNative(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "cliproxy":
		return h.handleCopilotNative(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "groq", "openai", "deepseek":
		return h.handleOpenAICompat(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "google_ai_studio":
		return h.handleGoogleAIStudio(w, r, routing, usageCtx, bodyBytes, startTime, username)
	default:
		http.Error(w, `{"error": "Unsupported provider type"}`, http.StatusNotImplemented)
		return 0, 0, false
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

func (h *Handler) handleOpenAICompat(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	// Replace model name in request body with the routed model
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		h.logger.Printf("[%s] JSON parse error: %v (body length=%d)", routing.ProviderType, err, len(bodyBytes))
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return 0, 0, false
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
	} else if routing.ProviderType == "deepseek" {
		path = "/chat/completions"
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
		return 0, 0, false
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
		return 0, 0, false
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
	cacheHit := false

	// Stream response directly while capturing tokens
	if isStream {
		inputTokens, outputTokens, hasToolCall, cacheHit = openaicompat.StreamTransform(resp.Body, w)
	} else {
		// Non-streaming: buffer to parse usage
		respBody, _ := io.ReadAll(resp.Body)
		w.Write(respBody)

		// Extract token usage from response
		inputTokens, outputTokens, cacheHit, _ = openaicompat.ExtractTokens(respBody)
		hasToolCall = openaicompat.DetectToolCall(respBody)
	}

	h.runnerLogger.Printf("OK [%s] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.ProviderType, routing.Model, isStream, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	h.modelLimiter.RecordTokens(routing.LLMModelID, inputTokens+outputTokens)
	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
	return inputTokens, outputTokens, cacheHit
}

func (h *Handler) handleCopilotNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
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
		return 0, 0, false
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
	return 0, 0, false
}

func (h *Handler) handleAntigravityNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	apiKey := h.resolveAPIKey(routing)

	resp, err := antigravity.ProxyToAntigravity(routing.BaseURL, apiKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [antigravity_proxy] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return 0, 0, false
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
		return 0, 0, false
	}

	if req.Stream {
		// Convert Anthropic SSE stream back to OpenAI SSE format for the client
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		in, out, hasToolCall, cacheHit := antigravity.StreamTransformToOpenAI(resp.Body, w, routing.Model)
		h.runnerLogger.Printf("OK [antigravity_proxy] model=%s stream=true tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		h.modelLimiter.RecordTokens(routing.LLMModelID, in+out)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
		return in, out, cacheHit
	} else {
		// Convert Anthropic response back to OpenAI format for the client
		respBytes, _ := io.ReadAll(resp.Body)
		transformed, in, out, hasToolCall, cacheHit, err := antigravity.TransformResponseToOpenAI(respBytes, routing.Model)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBytes)
			return 0, 0, false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(transformed)
		h.runnerLogger.Printf("OK [antigravity_proxy] model=%s stream=false tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		h.modelLimiter.RecordTokens(routing.LLMModelID, in+out)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
		return in, out, cacheHit
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

// extractAnthropicCacheHit returns true if the Anthropic response was served from prompt cache.
func extractAnthropicCacheHit(body []byte) bool {
	var resp struct {
		Usage struct {
			CacheReadInputTokens int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	return json.Unmarshal(body, &resp) == nil && resp.Usage.CacheReadInputTokens > 0
}

// --- Anthropic Messages API endpoint handlers ---

func (h *Handler) handleNativeUpstreamAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, user *database.User, originalModel string, bodyBytes []byte) (int, int, bool) {
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
		return h.handleAntigravityFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "cliproxy":
		return h.handleCopilotFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "groq", "openai", "deepseek":
		return h.handleOpenAICompatFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	case "google_ai_studio":
		return h.handleGoogleAIStudioFromAnthropic(w, r, routing, usageCtx, bodyBytes, startTime, username)
	default:
		http.Error(w, `{"error": {"type": "not_found_error", "message": "Unsupported provider type"}}`, http.StatusNotImplemented)
		return 0, 0, false
	}
}

// handleAntigravityFromAnthropic proxies an Anthropic-format request directly to an Anthropic-compatible upstream.
func (h *Handler) handleAntigravityFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	bodyBytes = h.orchestrateOrFallback(bodyBytes, routing, username)

	// Sanitize empty tool names before forwarding
	bodyBytes = anthropiccompat.SanitizeEmptyToolNames(bodyBytes)

	apiKey := h.resolveAPIKey(routing)

	// Use model-specific timeout for initial request
	timeouts := config.GetModelTimeouts(routing.Model)
	resp, err := anthropiccompat.ProxyDirectWithTimeout(routing.BaseURL, apiKey, bodyBytes, timeouts.RequestTimeout)
	if err != nil {
		h.runnerLogger.Printf("ERROR [antigravity_proxy/anthropic] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return 0, 0, false
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
		return 0, 0, false
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		in, out, cacheHit := antigravity.StreamTransform(resp.Body, w)
		h.runnerLogger.Printf("OK [antigravity_proxy/anthropic] model=%s stream=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		h.modelLimiter.RecordTokens(routing.LLMModelID, in+out)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
		return in, out, cacheHit
	} else {
		respBytes, _ := io.ReadAll(resp.Body)

		// Detect cache hit from original response before tool execution rewrites it
		cacheHit := extractAnthropicCacheHit(respBytes)

		// Execute tools if present and get updated response
		finalRespBytes, in, out, err := h.handleToolExecution(respBytes, routing, bodyBytes)
		if err != nil {
			h.runnerLogger.Printf("ERROR [tool_execution] model=%s err=%v", routing.Model, err)
			// Fall back to original response
			finalRespBytes = respBytes
			var anthropicResp struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(respBytes, &anthropicResp) == nil {
				in, out = anthropicResp.Usage.InputTokens, anthropicResp.Usage.OutputTokens
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(finalRespBytes)

		// Detect tool calls from original response
		hasToolCall := detectAnthropicToolCall(respBytes)

		h.runnerLogger.Printf("OK [antigravity_proxy/anthropic] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
			routing.Model, req.Stream, hasToolCall, in, out, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))
		h.modelLimiter.RecordTokens(routing.LLMModelID, in+out)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
		return in, out, cacheHit
	}
}

// handleCopilotFromAnthropic proxies an Anthropic-format request to a Copilot proxy (already speaks Anthropic format).
func (h *Handler) handleCopilotFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	// Replace model with routed model and inject system message
	bodyBytes = h.orchestrateOrFallback(bodyBytes, routing, username)

	// Deduplicate tool_result blocks with the same tool_use_id
	// The upstream OpenAI-compatible endpoint rejects duplicate tool_call_id values
	bodyBytes = anthropiccompat.DeduplicateToolResults(bodyBytes)

	// Sanitize empty tool names before forwarding
	bodyBytes = anthropiccompat.SanitizeEmptyToolNames(bodyBytes)

	resp, upstreamURL, err := copilot.ProxyToCopilot(routing.BaseURL, routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [cliproxy/anthropic] model=%s url=%s user=%s latency=%s err=%v", routing.Model, upstreamURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return 0, 0, false
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
	return 0, 0, false
}

// handleOpenAICompatFromAnthropic converts Anthropic→OpenAI, proxies, then converts response back to Anthropic.
func (h *Handler) handleOpenAICompatFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	// Check if this is a Claude Code request (client handles tools) vs proxy-injected tools
	isClaudeCode := anthropiccompat.IsClaudeCodeRequest(bodyBytes)

	// Convert Anthropic request to OpenAI format, preserving cache_control for OpenRouter
	isOpenRouter := strings.Contains(routing.BaseURL, "openrouter.ai")
	openaiBody, isStream, err := anthropiccompat.AnthropicToOpenAI(bodyBytes, isOpenRouter)
	if err != nil {
		h.logger.Printf("[%s/anthropic] conversion error: %v", routing.ProviderType, err)
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Invalid request body"}}`, http.StatusBadRequest)
		return 0, 0, false
	}

	// For proxy-injected tools, force non-streaming so tool execution works server-side
	needsToolExecution := !isClaudeCode
	if needsToolExecution && isStream {
		var body2 map[string]interface{}
		json.Unmarshal(openaiBody, &body2)
		body2["stream"] = false
		delete(body2, "stream_options")
		openaiBody, _ = json.Marshal(body2)
	}

	// Replace model with routed model
	var body map[string]interface{}
	json.Unmarshal(openaiBody, &body)
	body["model"] = routing.Model
	openaiBody, _ = json.Marshal(body)

	path := "/v1/chat/completions"
	if routing.ProviderType == "groq" {
		path = "/openai/v1/chat/completions"
	} else if routing.ProviderType == "deepseek" {
		path = "/chat/completions"
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
		return 0, 0, false
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [%s/anthropic] status=%d model=%s url=%s key=%s user=%s latency=%s body=%s", routing.ProviderType, resp.StatusCode, routing.Model, upstreamURL, keyHint, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return 0, 0, false
	}

	var inputTokens, outputTokens int
	hasToolCall := false
	cacheHit := false

	if isStream && !needsToolExecution {
		// Claude Code client with streaming — pass through directly
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		inputTokens, outputTokens, hasToolCall, cacheHit = anthropiccompat.OpenAIStreamToAnthropicStream(resp.Body, w, routing.Model)
	} else {
		// Non-streaming (or forced non-streaming for tool execution)
		respBody, _ := io.ReadAll(resp.Body)

		if needsToolExecution {
			// Execute tools if present and get Anthropic-format response
			anthropicResp, in, out, tc, err := h.handleToolExecutionOpenAI(respBody, routing, openaiBody, routing.Model, path)
			if err != nil {
				h.runnerLogger.Printf("ERROR [tool_execution] model=%s err=%v", routing.Model, err)
				// Fall back to direct conversion
				anthropicResp, in, out, tc, _, err = anthropiccompat.OpenAIResponseToAnthropic(respBody, routing.Model)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(resp.StatusCode)
					w.Write(respBody)
					return 0, 0, false
				}
			}
			inputTokens, outputTokens, hasToolCall = in, out, tc

			if isStream {
				// Client requested streaming — send as Anthropic SSE events
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				anthropiccompat.WriteAnthropicResponseAsSSE(anthropicResp, w, routing.Model)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(anthropicResp)
			}
		} else {
			anthropicResp, in, out, tc, ch, err := anthropiccompat.OpenAIResponseToAnthropic(respBody, routing.Model)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				w.Write(respBody)
				return 0, 0, false
			}
			inputTokens, outputTokens, hasToolCall, cacheHit = in, out, tc, ch
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(anthropicResp)
		}
	}

	h.runnerLogger.Printf("OK [%s/anthropic] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.ProviderType, routing.Model, isStream, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	h.modelLimiter.RecordTokens(routing.LLMModelID, inputTokens+outputTokens)
	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
	return inputTokens, outputTokens, cacheHit
}

// handleGoogleAIStudio handles OpenAI-format requests routed to Google AI Studio.
func (h *Handler) handleGoogleAIStudio(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	// Replace model in request with routed model
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
		return 0, 0, false
	}
	body["model"] = routing.Model
	bodyBytes, _ = json.Marshal(body)

	geminiBody, model, isStream, err := googleaistudio.OpenAIToGemini(bodyBytes)
	if err != nil {
		h.logger.Printf("[google_ai_studio] conversion error: %v", err)
		http.Error(w, `{"error": "Failed to convert request"}`, http.StatusBadRequest)
		return 0, 0, false
	}

	apiKey := h.resolveAPIKey(routing)

	resp, err := googleaistudio.Proxy(routing.BaseURL, apiKey, model, geminiBody, isStream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [google_ai_studio] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return 0, 0, false
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [google_ai_studio] status=%d model=%s url=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return 0, 0, false
	}

	var inputTokens, outputTokens int
	hasToolCall := false

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		inputTokens, outputTokens, hasToolCall = googleaistudio.StreamTransformToOpenAI(resp.Body, w, routing.Model)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		transformed, in, out, tc, err := googleaistudio.GeminiToOpenAI(respBody, routing.Model)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBody)
			return 0, 0, false
		}
		inputTokens, outputTokens, hasToolCall = in, out, tc
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(transformed)
	}

	h.runnerLogger.Printf("OK [google_ai_studio] model=%s stream=%v tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.Model, isStream, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	h.modelLimiter.RecordTokens(routing.LLMModelID, inputTokens+outputTokens)
	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
	return inputTokens, outputTokens, false // Google AI Studio does not expose prompt cache info
}

// handleGoogleAIStudioFromAnthropic handles Anthropic-format requests routed to Google AI Studio.
func (h *Handler) handleGoogleAIStudioFromAnthropic(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte, startTime time.Time, username string) (int, int, bool) {
	// Convert Anthropic request to OpenAI format first
	openaiBody, _, err := anthropiccompat.AnthropicToOpenAI(bodyBytes, false)
	if err != nil {
		h.logger.Printf("[google_ai_studio/anthropic] anthropic→openai conversion error: %v", err)
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Invalid request body"}}`, http.StatusBadRequest)
		return 0, 0, false
	}

	// Replace model with routed model
	var body map[string]interface{}
	json.Unmarshal(openaiBody, &body)
	body["model"] = routing.Model
	openaiBody, _ = json.Marshal(body)

	// Convert OpenAI to Gemini format
	geminiBody, model, _, err := googleaistudio.OpenAIToGemini(openaiBody)
	if err != nil {
		h.logger.Printf("[google_ai_studio/anthropic] openai→gemini conversion error: %v", err)
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Failed to convert request"}}`, http.StatusBadRequest)
		return 0, 0, false
	}

	apiKey := h.resolveAPIKey(routing)

	// Always use non-streaming for Anthropic clients going through double conversion
	resp, err := googleaistudio.Proxy(routing.BaseURL, apiKey, model, geminiBody, false)
	if err != nil {
		h.runnerLogger.Printf("ERROR [google_ai_studio/anthropic] model=%s url=%s user=%s latency=%s err=%v", routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), err)
		http.Error(w, `{"error": {"type": "api_error", "message": "Upstream request failed"}}`, http.StatusBadGateway)
		return 0, 0, false
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [google_ai_studio/anthropic] status=%d model=%s url=%s user=%s latency=%s body=%s", resp.StatusCode, routing.Model, routing.BaseURL, username, time.Since(startTime).Round(time.Millisecond), string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return 0, 0, false
	}

	// Gemini response → OpenAI format → Anthropic format
	geminiRespBody, _ := io.ReadAll(resp.Body)
	openaiResp, _, _, _, err := googleaistudio.GeminiToOpenAI(geminiRespBody, routing.Model)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(geminiRespBody)
		return 0, 0, false
	}

	anthropicResp, inputTokens, outputTokens, hasToolCall, _, err := anthropiccompat.OpenAIResponseToAnthropic(openaiResp, routing.Model)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(openaiResp)
		return 0, 0, false
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(anthropicResp)

	h.runnerLogger.Printf("OK [google_ai_studio/anthropic] model=%s stream=false tool_call=%v tokens=%d/%d latency=%s user=%s req_size=%d",
		routing.Model, hasToolCall,
		inputTokens, outputTokens, time.Since(startTime).Round(time.Millisecond), username, len(bodyBytes))

	h.modelLimiter.RecordTokens(routing.LLMModelID, inputTokens+outputTokens)
	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
	return inputTokens, outputTokens, false // Google AI Studio does not expose prompt cache info
}

func (h *Handler) orchestrateOrFallback(bodyBytes []byte, routing RoutingResult, username string) []byte {
	if anthropiccompat.IsClaudeCodeRequest(bodyBytes) {
		return anthropiccompat.InjectSystemMessage(bodyBytes, routing.Model)
	}

	apiKey := h.resolveAPIKey(routing)
	messages := extractMessagesForClassify(bodyBytes)
	if messages == nil {
		return anthropiccompat.InjectSystemMessage(bodyBytes, routing.Model)
	}

	pr := orchestrator.PhaseRequest{
		BaseURL:  routing.BaseURL,
		APIKey:   apiKey,
		Model:    routing.Model,
		Messages: messages,
	}

	classifyResult, err := h.orchestrator.Classify(pr)
	if err != nil {
		h.runnerLogger.Printf("[orchestrator] classify failed user=%s err=%v, falling back", username, err)
		return anthropiccompat.InjectSystemMessage(bodyBytes, routing.Model)
	}

	if classifyResult.Intent == "question" {
		return anthropiccompat.InjectSystemMessageOrchestrated(bodyBytes, routing.Model, "question", nil)
	}

	planResult, err := h.orchestrator.Plan(pr, classifyResult.Intent)
	if err != nil {
		h.runnerLogger.Printf("[orchestrator] plan failed user=%s err=%v, using classify intent only", username, err)
	}

	return anthropiccompat.InjectSystemMessageOrchestrated(bodyBytes, routing.Model, classifyResult.Intent, planResult)
}

func extractMessagesForClassify(bodyBytes []byte) []map[string]interface{} {
	var req struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil
	}
	if len(req.Messages) == 0 {
		return nil
	}
	last := req.Messages[len(req.Messages)-1]
	return []map[string]interface{}{last}
}
