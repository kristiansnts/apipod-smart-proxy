package proxy

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/antigravity"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/copilot"
)

func (h *Handler) handleNativeUpstream(w http.ResponseWriter, r *http.Request, routing RoutingResult, user *database.User, originalModel string, bodyBytes []byte) {
	usageCtx := database.UsageContext{
		QuotaItemID:      routing.QuotaItemID,
		UserID:           user.ID,
		RequestedModel:   originalModel,
		RoutedModel:      routing.Model,
		UpstreamProvider: "native:" + routing.ProviderType,
	}

	if routing.ProviderType == "antigravity_native" {
		h.handleAntigravityNative(w, r, routing, usageCtx, bodyBytes)
		return
	}

	if routing.ProviderType == "copilot_native" {
		h.handleCopilotNative(w, r, routing, usageCtx, bodyBytes)
		return
	}
	
	http.Error(w, `{"error": "Native provider not yet fully implemented"}`, http.StatusNotImplemented)
}

func (h *Handler) handleCopilotNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	// Copilot uses OpenAI format, so no transformation needed usually
	// But we check if it's an Anthropic request from user and transform if needed
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	resp, err := copilot.ProxyToCopilot(routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.logger.Printf("Copilot Error: %v", err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	
	// Logging for copilot is simpler as it returns standard OpenAI tokens
}


func (h *Handler) handleAntigravityNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	// 1. Transform request to Google format
	googleBody, err := antigravity.TransformAnthropicToGoogle(bodyBytes)
	if err != nil {
		http.Error(w, `{"error": "Failed to transform request"}`, http.StatusInternalServerError)
		return
	}

	// 2. Get Access Token (refresh_token stored in APIKey)
	accessToken, err := antigravity.ExchangeRefreshToken(routing.APIKey)
	if err != nil {
		h.logger.Printf("OAuth Error: %v", err)
		http.Error(w, `{"error": "Authentication failed with upstream"}`, http.StatusBadGateway)
		return
	}

	// 3. Determine if streaming
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	// 4. Proxy to Google Cloud Code
	resp, err := antigravity.ProxyToGoogle(accessToken, routing.Model, googleBody, req.Stream)
	if err != nil {
		h.logger.Printf("Upstream request failed: %v", err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 5. Handle Response
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
			http.Error(w, `{"error": "Failed to transform response"}`, http.StatusInternalServerError)
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
