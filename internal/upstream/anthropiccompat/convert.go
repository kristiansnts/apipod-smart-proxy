package anthropiccompat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	Tools       []interface{}     `json:"tools,omitempty"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
}

// ClaudeCodeRequest represents the full Claude Code request format
type ClaudeCodeRequest struct {
	Model       string            `json:"model"`
	Messages    []AnthropicMsg    `json:"messages"`
	Temperature float64           `json:"temperature"`
	System      []SystemMessage   `json:"system"`
	Tools       []interface{}     `json:"tools"`
	Metadata    map[string]string `json:"metadata"`
	MaxTokens   int               `json:"max_tokens"`
	Stream      bool              `json:"stream"`
}

type SystemMessage struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"`
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
	systemContent := ""
	if req.System != nil {
		systemContent = extractSystemText(req.System)
	}
	
	// Inject custom system message
	customSystemMsg, err := loadCustomSystemMessage()
	if err == nil && customSystemMsg != "" {
		if systemContent != "" {
			systemContent = customSystemMsg + "\n\n" + systemContent
		} else {
			systemContent = customSystemMsg
		}
	}
	
	if systemContent != "" {
		messages = append(messages, OpenAIMessage{Role: "system", Content: systemContent})
	}

	// Convert messages
	for _, m := range req.Messages {
		text := extractContentText(m.Content)
		messages = append(messages, OpenAIMessage{Role: m.Role, Content: text})
	}

	// Cap max_tokens to safe limits for different models
	maxTokens := getMaxTokensForModel(req.Model, req.MaxTokens)

	openaiReq := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     req.Stream,
	}
	
	// Add tools in OpenAI format for OpenAI-compatible endpoints
	tools, err := loadMCPToolsForOpenAI()
	if err == nil && len(tools) > 0 {
		openaiReq["tools"] = tools
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

func loadCustomSystemMessage() (string, error) {
	data, err := os.ReadFile("system_prompt.txt")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func loadMCPTools() ([]interface{}, error) {
	data, err := os.ReadFile("mcp_tools.json")
	if err != nil {
		return nil, err
	}
	
	var tools []interface{}
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	
	return tools, nil
}

func getMaxTokensForModel(model string, requestedTokens int) int {
	// Model-specific token limits
	modelLimits := map[string]int{
		// OpenAI models
		"gpt-3.5-turbo": 4096,
		"gpt-4": 8192,
		"gpt-4-turbo": 4096,
		"gpt-4o": 4096,
		"gpt-4o-mini": 16384,
		
		// Anthropic models
		"claude-3-haiku": 4096,
		"claude-3-sonnet": 4096,
		"claude-3-opus": 4096,
		"claude-3.5-sonnet": 8192,
		"claude-sonnet-4": 8192,
		"claude-sonnet-4.5": 8192,
		"claude-sonnet-4-5": 8192,
		
		// Groq/Other models
		"llama3-8b-8192": 8192,
		"llama3-70b-8192": 8192,
		"mixtral-8x7b-32768": 32768,
		"gemma-7b-it": 8192,
		
		// Moonshot/Kimi models
		"moonshotai/kimi-k2-instruct-0905": 16384,
		"moonshot-v1-8k": 8192,
		"moonshot-v1-32k": 32768,
		"moonshot-v1-128k": 128000,
	}
	
	// Default limits
	defaultLimit := 4096
	maxSafeLimit := 16384
	
	// Get model-specific limit
	modelLimit, exists := modelLimits[model]
	if !exists {
		// If model not found, use conservative default
		modelLimit = defaultLimit
	}
	
	// If no tokens requested, use reasonable default
	if requestedTokens <= 0 {
		return min(modelLimit, defaultLimit)
	}
	
	// Cap to model's maximum and safe global limit
	return min(requestedTokens, min(modelLimit, maxSafeLimit))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Convert MCP tools to OpenAI format
func loadMCPToolsForOpenAI() ([]interface{}, error) {
	tools, err := loadMCPTools()
	if err != nil {
		return nil, err
	}
	
	var openaiTools []interface{}
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			// Convert to OpenAI function format
			openaiTool := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": toolMap["name"],
					"description": toolMap["description"],
					"parameters": toolMap["input_schema"],
				},
			}
			openaiTools = append(openaiTools, openaiTool)
		}
	}
	
	return openaiTools, nil
}

// Convert MCP tools to Anthropic format (keep original for now)
func loadMCPToolsForAnthropic() ([]interface{}, error) {
	return loadMCPTools()
}

// Check if this is a Claude Code request format
func isClaudeCodeRequest(bodyBytes []byte) bool {
	var check struct {
		System []interface{} `json:"system"`
		Tools  []interface{} `json:"tools"`
	}
	if err := json.Unmarshal(bodyBytes, &check); err != nil {
		return false
	}
	// Claude Code requests have system as array and tools array
	return len(check.System) > 0 && len(check.Tools) > 0
}

// Inject system message into Claude Code request format
func injectIntoClaudeCodeRequest(bodyBytes []byte, model string) []byte {
	var req ClaudeCodeRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return bodyBytes
	}
	
	// Set the routed model
	req.Model = model
	
	// Cap max_tokens to safe limits
	req.MaxTokens = getMaxTokensForModel(model, req.MaxTokens)
	
	// Load and inject custom system message
	customSystemMsg, err := loadCustomSystemMessage()
	if err == nil && customSystemMsg != "" {
		// Add our system message at the beginning
		systemMsg := SystemMessage{
			Type: "text",
			Text: customSystemMsg,
			CacheControl: map[string]interface{}{
				"type": "ephemeral",
			},
		}
		// Prepend our system message
		req.System = append([]SystemMessage{systemMsg}, req.System...)
	}
	
	// The tools are already in the request, so we don't need to inject them
	// Claude Code handles tools properly
	
	modified, _ := json.Marshal(req)
	return modified
}

func InjectSystemMessage(bodyBytes []byte, model string) []byte {
	// Check if body is empty
	if len(bodyBytes) == 0 {
		return bodyBytes
	}
	
	// Try to detect if this is a Claude Code request format
	if isClaudeCodeRequest(bodyBytes) {
		return injectIntoClaudeCodeRequest(bodyBytes, model)
	}
	
	var req AnthropicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		// Log error but return original body to avoid breaking the request
		return bodyBytes
	}

	// Replace model with routed model
	req.Model = model
	
	// Cap max_tokens to safe limits
	req.MaxTokens = getMaxTokensForModel(model, req.MaxTokens)

	// Handle system message injection
	systemContent := ""
	if req.System != nil {
		systemContent = extractSystemText(req.System)
	}
	
	// Inject custom system message
	customSystemMsg, err := loadCustomSystemMessage()
	if err == nil && customSystemMsg != "" {
		if systemContent != "" {
			systemContent = customSystemMsg + "\n\n" + systemContent
		} else {
			systemContent = customSystemMsg
		}
	}
	
	if systemContent != "" {
		// Properly encode system content as JSON string
		systemJSON, err := json.Marshal(systemContent)
		if err == nil {
			req.System = json.RawMessage(systemJSON)
		}
	}
	
	// Inject MCP tools in Anthropic format for direct Anthropic requests
	tools, err := loadMCPToolsForAnthropic()
	if err == nil && len(tools) > 0 {
		req.Tools = tools
	}

	modified, _ := json.Marshal(req)
	return modified
}
