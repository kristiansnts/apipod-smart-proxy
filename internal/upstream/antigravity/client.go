package antigravity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
	"fmt"
)

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     120 * time.Second,
}

func ExchangeRefreshToken(refreshToken string) (string, error) {
	return "internal-managed", nil
}

func ProxyToGoogle(accessToken string, model string, body []byte, stream bool) (*http.Response, error) {
	var openAIReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &openAIReq)

	// Translate OpenAI "Content String" to Anthropic "Content Array" (THE FIX)
	anthropicMessages := make([]map[string]interface{}, 0)
	for _, m := range openAIReq.Messages {
		msg := map[string]interface{}{
			"role": m.Role,
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": m.Content,
				},
			},
		}
		anthropicMessages = append(anthropicMessages, msg)
	}

	upstreamReq := map[string]interface{}{
		"model":             model,
		"messages":          anthropicMessages,
		"max_tokens":        4096,
		"stream":            stream,
	}

	finalBody, _ := json.Marshal(upstreamReq)
	apiURL := "http://localhost:8080/v1/messages"
	
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(finalBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	// Translation Layer: Badri -> OpenAI (So Cursor/UI works)
	if !stream && resp.StatusCode == 200 {
		respBody, _ := io.ReadAll(resp.Body)
		var anthropicResp struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			ID string `json:"id"`
		}
		
		if err := json.Unmarshal(respBody, &anthropicResp); err == nil && len(anthropicResp.Content) > 0 {
			openAIResp := map[string]interface{}{
				"id":      anthropicResp.ID,
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": anthropicResp.Content[0].Text,
						},
						"finish_reason": "stop",
						"index":         0,
					},
				},
			}
			newBody, _ := json.Marshal(openAIResp)
			resp.Body = io.NopCloser(bytes.NewReader(newBody))
			resp.ContentLength = int64(len(newBody))
			resp.Header.Set("Content-Length", fmt.Sprint(len(newBody)))
			resp.Header.Set("Content-Type", "application/json")
		} else {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
		}
	}

	return resp, nil
}
