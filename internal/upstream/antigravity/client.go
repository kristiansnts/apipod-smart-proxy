package antigravity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
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

// ProxyToAntigravity sends the request to the upstream Antigravity engine using
// the base URL and API key from the database.
func ProxyToAntigravity(baseURL string, apiKey string, model string, body []byte, stream bool) (*http.Response, error) {
	// 1. Parse incoming OpenAI body
	var openAIReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	}
	json.Unmarshal(body, &openAIReq)

	// 2. Convert to Anthropic format for the upstream /v1/messages
	type AnthropicMsg struct {
		Role    string                   `json:"role"`
		Content []map[string]interface{} `json:"content"`
	}
	var anthropicMsgs []AnthropicMsg
	for _, m := range openAIReq.Messages {
		anthropicMsgs = append(anthropicMsgs, AnthropicMsg{
			Role: m.Role,
			Content: []map[string]interface{}{
				{"type": "text", "text": m.Content},
			},
		})
	}

	upstreamBody := map[string]interface{}{
		"model":      model,
		"messages":   anthropicMsgs,
		"max_tokens": 1024,
	}

	finalBody, _ := json.Marshal(upstreamBody)
	apiURL := strings.TrimRight(baseURL, "/") + "/v1/messages"

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(finalBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	return client.Do(req)
}
