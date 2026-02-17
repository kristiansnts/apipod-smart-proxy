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

// ProxyToAntigravity converts an OpenAI chat completions request to Anthropic Messages format
// (including tools, tool_calls, and tool results) and sends it to the upstream.
func ProxyToAntigravity(baseURL string, apiKey string, model string, body []byte, stream bool) (*http.Response, error) {
	// Parse full OpenAI request
	var openAIReq struct {
		Messages    []json.RawMessage        `json:"messages"`
		MaxTokens   int                      `json:"max_tokens"`
		Temperature *float64                 `json:"temperature,omitempty"`
		TopP        *float64                 `json:"top_p,omitempty"`
		Stream      bool                     `json:"stream"`
		Tools       []map[string]interface{} `json:"tools,omitempty"`
		Stop        interface{}              `json:"stop,omitempty"`
	}
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		// Fallback: send as-is
		return proxyRaw(baseURL, apiKey, body)
	}

	// Convert messages
	var anthropicMsgs []map[string]interface{}
	var systemContent string

	for _, rawMsg := range openAIReq.Messages {
		var msg struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
			ToolCallID string          `json:"tool_call_id,omitempty"`
			Name       string          `json:"name,omitempty"`
		}
		if json.Unmarshal(rawMsg, &msg) != nil {
			continue
		}

		switch msg.Role {
		case "system":
			// Extract system message
			var s string
			if json.Unmarshal(msg.Content, &s) == nil {
				if systemContent != "" {
					systemContent += "\n\n"
				}
				systemContent += s
			}

		case "user":
			content := convertContentToAnthropic(msg.Content)
			anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
				"role":    "user",
				"content": content,
			})

		case "assistant":
			var contentBlocks []map[string]interface{}

			// Add text content if present
			var textContent string
			if json.Unmarshal(msg.Content, &textContent) == nil && textContent != "" {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": textContent,
				})
			}

			// Convert tool_calls to tool_use blocks
			for _, tc := range msg.ToolCalls {
				var inputParsed interface{}
				if json.Unmarshal([]byte(tc.Function.Arguments), &inputParsed) != nil {
					inputParsed = map[string]interface{}{}
				}
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": inputParsed,
				})
			}

			if len(contentBlocks) == 0 {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": "",
				})
			}

			anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
				"role":    "assistant",
				"content": contentBlocks,
			})

		case "tool":
			// OpenAI tool result → Anthropic tool_result block
			var resultText string
			if json.Unmarshal(msg.Content, &resultText) != nil {
				resultText = string(msg.Content)
			}

			anthropicMsgs = append(anthropicMsgs, map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     resultText,
					},
				},
			})
		}
	}

	// Merge consecutive user messages (Anthropic requires alternating roles)
	anthropicMsgs = mergeConsecutiveUserMessages(anthropicMsgs)

	// Build Anthropic request
	maxTokens := openAIReq.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	upstreamBody := map[string]interface{}{
		"model":      model,
		"messages":   anthropicMsgs,
		"max_tokens": maxTokens,
		"stream":     stream,
	}

	if systemContent != "" {
		upstreamBody["system"] = systemContent
	}

	// Convert OpenAI tools to Anthropic format
	if len(openAIReq.Tools) > 0 {
		var anthropicTools []map[string]interface{}
		for _, tool := range openAIReq.Tools {
			fn, ok := tool["function"].(map[string]interface{})
			if !ok {
				continue
			}
			anthropicTool := map[string]interface{}{
				"name": fn["name"],
			}
			if desc, ok := fn["description"]; ok {
				anthropicTool["description"] = desc
			}
			if params, ok := fn["parameters"]; ok {
				anthropicTool["input_schema"] = params
			} else {
				anthropicTool["input_schema"] = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}
			}
			anthropicTools = append(anthropicTools, anthropicTool)
		}
		if len(anthropicTools) > 0 {
			upstreamBody["tools"] = anthropicTools
		}
	}

	if openAIReq.Temperature != nil {
		upstreamBody["temperature"] = *openAIReq.Temperature
	}
	if openAIReq.TopP != nil {
		upstreamBody["top_p"] = *openAIReq.TopP
	}

	// Convert stop sequences
	if openAIReq.Stop != nil {
		switch v := openAIReq.Stop.(type) {
		case string:
			upstreamBody["stop_sequences"] = []string{v}
		case []interface{}:
			var seqs []string
			for _, s := range v {
				if str, ok := s.(string); ok {
					seqs = append(seqs, str)
				}
			}
			if len(seqs) > 0 {
				upstreamBody["stop_sequences"] = seqs
			}
		}
	}

	finalBody, _ := json.Marshal(upstreamBody)
	return proxyRaw(baseURL, apiKey, finalBody)
}

func proxyRaw(baseURL string, apiKey string, body []byte) (*http.Response, error) {
	apiURL := strings.TrimRight(baseURL, "/") + "/v1/messages"

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	return client.Do(req)
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// convertContentToAnthropic converts OpenAI content (string or array) to Anthropic content blocks.
func convertContentToAnthropic(raw json.RawMessage) interface{} {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []map[string]interface{}{
			{"type": "text", "text": s},
		}
	}

	// Array content (multimodal)
	var parts []map[string]interface{}
	if json.Unmarshal(raw, &parts) == nil {
		var blocks []map[string]interface{}
		for _, part := range parts {
			pType, _ := part["type"].(string)
			switch pType {
			case "text":
				blocks = append(blocks, map[string]interface{}{
					"type": "text",
					"text": part["text"],
				})
			case "image_url":
				// Pass through image content if present
				blocks = append(blocks, part)
			default:
				blocks = append(blocks, part)
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
	}

	return []map[string]interface{}{
		{"type": "text", "text": string(raw)},
	}
}

// mergeConsecutiveUserMessages merges consecutive user-role messages into one,
// since Anthropic requires strictly alternating user/assistant roles.
func mergeConsecutiveUserMessages(msgs []map[string]interface{}) []map[string]interface{} {
	if len(msgs) <= 1 {
		return msgs
	}

	var result []map[string]interface{}
	for _, msg := range msgs {
		role, _ := msg["role"].(string)

		if len(result) > 0 {
			prevRole, _ := result[len(result)-1]["role"].(string)
			if role == "user" && prevRole == "user" {
				// Merge content arrays
				prevContent := toContentArray(result[len(result)-1]["content"])
				curContent := toContentArray(msg["content"])
				result[len(result)-1]["content"] = append(prevContent, curContent...)
				continue
			}
		}
		result = append(result, msg)
	}
	return result
}

func toContentArray(content interface{}) []map[string]interface{} {
	if arr, ok := content.([]map[string]interface{}); ok {
		return arr
	}
	if s, ok := content.(string); ok {
		return []map[string]interface{}{{"type": "text", "text": s}}
	}
	return nil
}
