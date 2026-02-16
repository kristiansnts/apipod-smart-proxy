package proxy

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/antigravity"
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

	switch routing.ProviderType {
	case "antigravity_proxy":
		h.handleAntigravityNative(w, r, routing, usageCtx, bodyBytes)
	case "cliproxy":
		h.handleCopilotNative(w, r, routing, usageCtx, bodyBytes)
	case "groq", "openai":
		h.handleOpenAICompat(w, r, routing, usageCtx, bodyBytes)
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

func (h *Handler) handleOpenAICompat(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
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
		h.runnerLogger.Printf("ERROR [%s] model=%s url=%s key=%s err=%v", routing.ProviderType, routing.Model, upstreamURL, keyHint, err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	// Handle error responses - always buffer to log
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [%s] status=%d model=%s url=%s key=%s body=%s", routing.ProviderType, resp.StatusCode, routing.Model, upstreamURL, keyHint, string(respBody))
		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	h.runnerLogger.Printf("OK [%s] model=%s stream=%v", routing.ProviderType, routing.Model, isStream)

	// Copy headers
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	var inputTokens, outputTokens int

	// Stream response directly while capturing tokens
	if isStream {
		inputTokens, outputTokens = openaicompat.StreamTransform(resp.Body, w)
	} else {
		// Non-streaming: buffer to parse usage
		respBody, _ := io.ReadAll(resp.Body)
		w.Write(respBody)
		
		// Extract token usage from response
		inputTokens, outputTokens, _ = openaicompat.ExtractTokens(respBody)
	}

	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, inputTokens, outputTokens)
	}
}

func (h *Handler) handleCopilotNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	resp, upstreamURL, err := copilot.ProxyToCopilot(routing.BaseURL, routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		keyPrefix := "unknown"
		if len(routing.APIKey) > 6 {
			keyPrefix = routing.APIKey[:4] + "..." + routing.APIKey[len(routing.APIKey)-3:]
		}
		h.runnerLogger.Printf("ERROR [cliproxy] model=%s url=%s key=%s err=%v", routing.Model, upstreamURL, keyPrefix, err)
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
		h.runnerLogger.Printf("ERROR [cliproxy] status=%d model=%s url=%s key=%s body=%s", resp.StatusCode, routing.Model, upstreamURL, keyPrefix, string(respBody))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	} else {
		h.runnerLogger.Printf("OK [cliproxy] model=%s", routing.Model)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func (h *Handler) handleAntigravityNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	apiKey := h.resolveAPIKey(routing)

	resp, err := antigravity.ProxyToAntigravity(routing.BaseURL, apiKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.runnerLogger.Printf("ERROR [antigravity_proxy] model=%s url=%s err=%v", routing.Model, routing.BaseURL, err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		h.runnerLogger.Printf("ERROR [antigravity_proxy] status=%d model=%s url=%s body=%s", resp.StatusCode, routing.Model, routing.BaseURL, string(respBody))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, 0, 0)
		}
		return
	}

	h.runnerLogger.Printf("OK [antigravity_proxy] model=%s", routing.Model)

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		in, out := antigravity.StreamTransform(resp.Body, w)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	} else {
		respBytes, _ := io.ReadAll(resp.Body)
		transformed, in, out, err := antigravity.TransformResponse(respBytes, routing.Model)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBytes)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(transformed)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, in, out)
		}
	}
}
