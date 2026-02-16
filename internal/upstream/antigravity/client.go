package antigravity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     120 * time.Second,
}

func ExchangeRefreshToken(refreshToken string) (string, error) {
	return "internal-managed", nil
}

// ProxyToGoogle now accepts the internal API key directly
func ProxyToGoogle(antigravityInternalAPIKey string, model string, body []byte, stream bool) (*http.Response, error) {
	// 1. Parse incoming OpenAI body
	var openAIReg struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	}
	json.Unmarshal(body, &openAIReg)

	// 2. Convert to Anthropic format for the Rust Engine /v1/messages
	type AnthropicMsg struct {
		Role    string `json:"role"`
		Content []map[string]interface{} `json:"content"`
	}
	var anthropicMsgs []AnthropicMsg
	for _, m := range openAIReg.Messages {
		anthropicMsgs = append(anthropicMsgs, AnthropicMsg{
			Role: m.Role,
			Content: []map[string]interface{}{
				{"type": "text", "text": m.Content},
			},
		})
	}

	upstreamReq := map[string]interface{}{
		"model":      "gemini-3-flash", // Hardcoded for now based on previous discussion
		"messages":   anthropicMsgs,
		"max_tokens": 1024,
	}
	
	finalBody, _ := json.Marshal(upstreamReq)
	apiURL := "http://localhost:8045/v1/messages" // Rust engine is always on localhost:8045
	
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(finalBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", antigravityInternalAPIKey) // Use the passed API key
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	return client.Do(req)
}
