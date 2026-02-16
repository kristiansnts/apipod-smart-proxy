package anthropiccompat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicRequest represents an Anthropic Messages API request
type AnthropicRequest struct {
	Model       string            `json:"model"`
	Messages    []AnthropicMsg    `json:"messages"`
	System      json.RawMessage   `json:"system,omitempty"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature *float64          `json:"temperature,omitempty"`
	TopP        *float64          `json:"top_p,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
}

type AnthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AnthropicToOpenAI converts an Anthropic Messages request body to an OpenAI chat completions request body.
func AnthropicToOpenAI(body []byte) ([]byte, bool, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, false, err
	}

	type OpenAIMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []OpenAIMessage

	// Handle system prompt
	if req.System != nil {
		systemText := extractSystemText(req.System)
		if systemText != "" {
			messages = append(messages, OpenAIMessage{Role: "system", Content: systemText})
		}
	}

	// Convert messages
	for _, m := range req.Messages {
		text := extractContentText(m.Content)
		messages = append(messages, OpenAIMessage{Role: m.Role, Content: text})
	}

	openaiReq := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
		"stream":     req.Stream,
	}
	if req.Temperature != nil {
		openaiReq["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openaiReq["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		openaiReq["stop"] = req.StopSequences
	}
	// Request usage in streaming mode so we can extract tokens
	if req.Stream {
		openaiReq["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	out, err := json.Marshal(openaiReq)
	return out, req.Stream, err
}

// extractSystemText handles system as either a string or array of content blocks.
func extractSystemText(raw json.RawMessage) string {
	// Try string first
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try array of content blocks
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// extractContentText handles content as either a string or array of content blocks.
func extractContentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// OpenAIResponseToAnthropic converts an OpenAI chat completions response to Anthropic Messages format.
func OpenAIResponseToAnthropic(body []byte, model string) ([]byte, int, int, error) {
	var openaiResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, 0, 0, err
	}

	content := ""
	stopReason := "end_turn"
	if len(openaiResp.Choices) > 0 {
		content = openaiResp.Choices[0].Message.Content
		switch openaiResp.Choices[0].FinishReason {
		case "length":
			stopReason = "max_tokens"
		case "stop":
			stopReason = "end_turn"
		}
	}

	anthropicResp := map[string]interface{}{
		"id":   openaiResp.ID,
		"type": "message",
		"role": "assistant",
		"model": model,
		"content": []map[string]interface{}{
			{"type": "text", "text": content},
		},
		"stop_reason": stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  openaiResp.Usage.PromptTokens,
			"output_tokens": openaiResp.Usage.CompletionTokens,
		},
	}

	out, err := json.Marshal(anthropicResp)
	return out, openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, err
}

// OpenAIStreamToAnthropicStream converts an OpenAI SSE stream to Anthropic SSE stream format.
// Returns input and output token counts captured from the stream.
func OpenAIStreamToAnthropicStream(r io.Reader, w io.Writer, model string) (int, int) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))
	inputTokens, outputTokens := 0, 0
	started := false

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}

		var chunk struct {
			ID      string `json:"id"`
			Choices []struct {
				Delta struct {
					Role    string `json:"role,omitempty"`
					Content string `json:"content,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Capture usage from final chunk
		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}

		// Emit message_start event once
		if !started {
			started = true
			msgStart := map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":    chunk.ID,
					"type":  "message",
					"role":  "assistant",
					"model": model,
					"content": []interface{}{},
					"usage": map[string]interface{}{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			}
			writeSSE(w, msgStart)

			// Emit content_block_start
			blockStart := map[string]interface{}{
				"type":  "content_block_start",
				"index": 0,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			}
			writeSSE(w, blockStart)
		}

		// Emit content_block_delta for text content
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			delta := map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": chunk.Choices[0].Delta.Content,
				},
			}
			writeSSE(w, delta)
		}

		// Emit stop events on finish
		if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil {
			stopReason := "end_turn"
			if *chunk.Choices[0].FinishReason == "length" {
				stopReason = "max_tokens"
			}

			blockStop := map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			}
			writeSSE(w, blockStop)

			msgDelta := map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason": stopReason,
				},
				"usage": map[string]interface{}{
					"output_tokens": outputTokens,
				},
			}
			writeSSE(w, msgDelta)

			msgStop := map[string]interface{}{
				"type": "message_stop",
			}
			writeSSE(w, msgStop)
		}
	}

	return inputTokens, outputTokens
}

// ProxyDirect creates an HTTP request to an Anthropic-compatible upstream,
// forwarding the body as-is with Anthropic auth headers.
func ProxyDirect(baseURL string, apiKey string, body []byte) (*http.Response, error) {
	apiURL := strings.TrimRight(baseURL, "/") + "/v1/messages"

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
	}
	return client.Do(req)
}

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     120 * time.Second,
}

func writeSSE(w io.Writer, event interface{}) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", getEventType(event), string(data))
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func getEventType(event interface{}) string {
	if m, ok := event.(map[string]interface{}); ok {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return "unknown"
}
