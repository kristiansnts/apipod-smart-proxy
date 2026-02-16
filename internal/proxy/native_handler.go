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

func (h *Handler) handleOpenAICompat(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	path := "/v1/chat/completions"
	if routing.ProviderType == "groq" {
		path = "/openai/v1/responses"
	}

	resp, err := openaicompat.Proxy(routing.BaseURL, routing.APIKey, path, bodyBytes)
	if err != nil {
		h.logger.Printf("OpenAI-compat (%s) Error: %v", routing.ProviderType, err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	// Copy upstream headers
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	if usageCtx.QuotaItemID > 0 {
		h.db.LogUsage(usageCtx, 0, 0)
	}
}

func (h *Handler) handleCopilotNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
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
}

func (h *Handler) handleAntigravityNative(w http.ResponseWriter, r *http.Request, routing RoutingResult, usageCtx database.UsageContext, bodyBytes []byte) {
	var req struct{ Stream bool `json:"stream"` }
	json.Unmarshal(bodyBytes, &req)

	resp, err := antigravity.ProxyToAntigravity(routing.BaseURL, routing.APIKey, routing.Model, bodyBytes, req.Stream)
	if err != nil {
		h.logger.Printf("Upstream request failed: %v", err)
		http.Error(w, `{"error": "Upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	usageCtx.StatusCode = resp.StatusCode

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		if usageCtx.QuotaItemID > 0 {
			h.db.LogUsage(usageCtx, 0, 0)
		}
		return
	}

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
