package anthropiccompat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/orchestrator"
)

var validToolNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

type AnthropicRequest struct {
	Model         string          `json:"model"`
	Messages      []AnthropicMsg  `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []interface{}   `json:"tools,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

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

type FullContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          interface{}      `json:"content,omitempty"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

// sanitizeToolName ensures a tool name matches ^[a-zA-Z0-9_-]+$ as required by OpenAI-compatible APIs.
func sanitizeToolName(name string) string {
	if name == "" {
		return "_unknown"
	}
	sanitized := validToolNameRe.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_unknown"
	}
	return sanitized
}

// SanitizeEmptyToolNames replaces empty or invalid tool_use names and their corresponding
// tool_result references with sanitized versions to prevent upstream
// APIs (Gemini, OpenAI) from rejecting the request.
func SanitizeEmptyToolNames(bodyBytes []byte) []byte {
	var fullReq map[string]interface{}
	if json.Unmarshal(bodyBytes, &fullReq) != nil {
		return bodyBytes
	}

	msgs, ok := fullReq["messages"].([]interface{})
	if !ok {
		return bodyBytes
	}

	modified := false
	for _, rawMsg := range msgs {
		msgMap, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		contentArr, ok := msgMap["content"].([]interface{})
		if !ok {
			continue
		}
		for _, block := range contentArr {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			bType, _ := blockMap["type"].(string)
			if bType == "tool_use" {
				name, _ := blockMap["name"].(string)
				sanitized := sanitizeToolName(name)
				if sanitized != name {
					blockMap["name"] = sanitized
					modified = true
				}
			}
		}
	}

	if !modified {
		return bodyBytes
	}

	result, err := json.Marshal(fullReq)
	if err != nil {
		return bodyBytes
	}
	return result
}

func DeduplicateToolResults(bodyBytes []byte) []byte {
	var req struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if json.Unmarshal(bodyBytes, &req) != nil {
		return bodyBytes
	}

	type location struct {
		msgIdx   int
		blockIdx int
	}
	seen := make(map[string][]location)

	type contentBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id,omitempty"`
	}

	for i, rawMsg := range req.Messages {
		var msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(rawMsg, &msg) != nil {
			continue
		}

		var blocks []contentBlock
		if json.Unmarshal(msg.Content, &blocks) != nil {
			continue
		}

		for j, block := range blocks {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				seen[block.ToolUseID] = append(seen[block.ToolUseID], location{i, j})
			}
		}
	}

	hasDuplicates := false
	type removeKey struct{ msgIdx, blockIdx int }
	toRemove := make(map[removeKey]bool)

	for _, locs := range seen {
		if len(locs) > 1 {
			hasDuplicates = true
			for _, loc := range locs[:len(locs)-1] {
				toRemove[removeKey{loc.msgIdx, loc.blockIdx}] = true
			}
		}
	}

	if !hasDuplicates {
		return bodyBytes
	}

	var fullReq map[string]interface{}
	if json.Unmarshal(bodyBytes, &fullReq) != nil {
		return bodyBytes
	}

	msgs, ok := fullReq["messages"].([]interface{})
	if !ok {
		return bodyBytes
	}

	var newMsgs []interface{}
	for i, rawMsg := range msgs {
		msgMap, ok := rawMsg.(map[string]interface{})
		if !ok {
			newMsgs = append(newMsgs, rawMsg)
			continue
		}

		contentArr, ok := msgMap["content"].([]interface{})
		if !ok {
			newMsgs = append(newMsgs, rawMsg)
			continue
		}

		var newContent []interface{}
		for j, block := range contentArr {
			if toRemove[removeKey{i, j}] {
				continue
			}
			newContent = append(newContent, block)
		}

		if len(newContent) == 0 {
			continue
		}

		newMsg := make(map[string]interface{})
		for k, v := range msgMap {
			newMsg[k] = v
		}
		newMsg["content"] = newContent
		newMsgs = append(newMsgs, newMsg)
	}

	fullReq["messages"] = newMsgs
	result, err := json.Marshal(fullReq)
	if err != nil {
		return bodyBytes
	}
	return result
}

// AnthropicToOpenAI converts an Anthropic Messages request body to OpenAI chat completions format,
// properly handling tool_use and tool_result content blocks.
func AnthropicToOpenAI(body []byte) ([]byte, bool, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, false, err
	}

	var messages []OpenAIMessage

	systemContent := ""
	if req.System != nil {
		systemContent = extractSystemText(req.System)
	}

	isClaudeCode := len(req.Tools) > 0
	if !isClaudeCode {
		customSystemMsg, err := loadCustomSystemMessage()
		if err == nil && customSystemMsg != "" {
			if systemContent != "" {
				systemContent = customSystemMsg + "\n\n" + systemContent
			} else {
				systemContent = customSystemMsg
			}
		}
	}

	if systemContent != "" {
		messages = append(messages, OpenAIMessage{Role: "system", Content: systemContent})
	}

	for _, m := range req.Messages {
		converted := convertAnthropicMessageToOpenAI(m)
		messages = append(messages, converted...)
	}

	maxTokens := req.MaxTokens
	if !isClaudeCode {
		maxTokens = getMaxTokensForModel(req.Model, req.MaxTokens)
	}

	openaiReq := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     req.Stream,
	}

	if len(req.Tools) > 0 {
		openaiReq["tools"] = convertAnthropicToolsToOpenAI(req.Tools)
	} else {
		tools, err := loadMCPToolsForOpenAI()
		if err == nil && len(tools) > 0 {
			openaiReq["tools"] = tools
		}
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
	if req.Stream {
		openaiReq["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	out, err := json.Marshal(openaiReq)
	return out, req.Stream, err
}

func convertAnthropicMessageToOpenAI(m AnthropicMsg) []OpenAIMessage {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []OpenAIMessage{{Role: m.Role, Content: s}}
	}

	var blocks []FullContentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return []OpenAIMessage{{Role: m.Role, Content: string(m.Content)}}
	}

	if m.Role == "assistant" {
		return convertAssistantBlocks(blocks)
	}

	if m.Role == "user" {
		return convertUserBlocks(blocks)
	}

	text := extractTextFromBlocks(blocks)
	return []OpenAIMessage{{Role: m.Role, Content: text}}
}

func convertAssistantBlocks(blocks []FullContentBlock) []OpenAIMessage {
	var textParts []string
	var thinkingParts []string
	var toolCalls []OpenAIToolCall

	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			if b.Thinking != "" {
				thinkingParts = append(thinkingParts, b.Thinking)
			}
		case "text":
			if b.Text != "" {
				textParts = append(textParts, b.Text)
			}
		case "tool_use":
			argsBytes, _ := json.Marshal(b.Input)
			if argsBytes == nil {
				argsBytes = []byte("{}")
			}
			name := sanitizeToolName(b.Name)
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   b.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      name,
					Arguments: string(argsBytes),
				},
			})
		}
	}

	msg := OpenAIMessage{Role: "assistant"}
	if len(thinkingParts) > 0 {
		reasoning := strings.Join(thinkingParts, "\n")
		msg.ReasoningContent = &reasoning
	}
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "\n")
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return []OpenAIMessage{msg}
}

func convertUserBlocks(blocks []FullContentBlock) []OpenAIMessage {
	var msgs []OpenAIMessage
	var textParts []string

	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_result":
			if len(textParts) > 0 {
				msgs = append(msgs, OpenAIMessage{Role: "user", Content: strings.Join(textParts, "\n")})
				textParts = nil
			}
			resultContent := extractToolResultContent(b.Content)
			msgs = append(msgs, OpenAIMessage{
				Role:       "tool",
				Content:    resultContent,
				ToolCallID: b.ToolUseID,
			})
		}
	}

	if len(textParts) > 0 {
		msgs = append(msgs, OpenAIMessage{Role: "user", Content: strings.Join(textParts, "\n")})
	}

	if len(msgs) == 0 {
		msgs = append(msgs, OpenAIMessage{Role: "user", Content: ""})
	}

	return msgs
}

func extractToolResultContent(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
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

func extractTextFromBlocks(blocks []FullContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func convertAnthropicToolsToOpenAI(tools []interface{}) []interface{} {
	var openaiTools []interface{}
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		openaiTool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        toolMap["name"],
				"description": toolMap["description"],
			},
		}
		fn := openaiTool["function"].(map[string]interface{})
		if schema, ok := toolMap["input_schema"]; ok {
			fn["parameters"] = schema
		}
		openaiTools = append(openaiTools, openaiTool)
	}
	return openaiTools
}

func extractSystemText(raw json.RawMessage) string {
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
	return ""
}

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

// OpenAIResponseToAnthropic converts an OpenAI chat completions response to Anthropic Messages format,
// including tool_calls conversion to tool_use content blocks.
func OpenAIResponseToAnthropic(body []byte, model string) ([]byte, int, int, bool, error) {
	var openaiResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content          *string         `json:"content"`
				ReasoningContent *string         `json:"reasoning_content,omitempty"`
				ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, 0, 0, false, err
	}

	var contentBlocks []map[string]interface{}
	stopReason := "end_turn"
	hasToolCall := false

	if len(openaiResp.Choices) > 0 {
		choice := openaiResp.Choices[0]

		switch choice.FinishReason {
		case "length":
			stopReason = "max_tokens"
		case "stop":
			stopReason = "end_turn"
		case "tool_calls":
			stopReason = "tool_use"
			hasToolCall = true
		}

		if choice.Message.ReasoningContent != nil && *choice.Message.ReasoningContent != "" {
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type":     "thinking",
				"thinking": *choice.Message.ReasoningContent,
			})
		}

		if choice.Message.Content != nil && *choice.Message.Content != "" {
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type": "text",
				"text": *choice.Message.Content,
			})
		}

		for _, tc := range choice.Message.ToolCalls {
			hasToolCall = true
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
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": "",
		})
	}

	anthropicResp := map[string]interface{}{
		"id":          openaiResp.ID,
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     contentBlocks,
		"stop_reason": stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  openaiResp.Usage.PromptTokens,
			"output_tokens": openaiResp.Usage.CompletionTokens,
		},
	}

	out, err := json.Marshal(anthropicResp)
	return out, openaiResp.Usage.PromptTokens, openaiResp.Usage.CompletionTokens, hasToolCall, err
}

// OpenAIStreamToAnthropicStream converts an OpenAI SSE stream to Anthropic SSE stream format,
// including tool_call streaming deltas.
func OpenAIStreamToAnthropicStream(r io.Reader, w io.Writer, model string) (int, int, bool) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))
	inputTokens, outputTokens := 0, 0
	hasToolCall := false
	started := false
	blockIndex := 0
	thinkingBlockStarted := false
	textBlockStarted := false

	type toolCallAccum struct {
		ID        string
		Name      string
		Arguments string
	}
	var pendingToolCalls []toolCallAccum

	type streamChunk struct {
		ID      string `json:"id"`
		Choices []struct {
			Delta struct {
				Role             string `json:"role,omitempty"`
				Content          string `json:"content,omitempty"`
				ReasoningContent string `json:"reasoning_content,omitempty"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id,omitempty"`
					Type     string `json:"type,omitempty"`
					Function struct {
						Name      string `json:"name,omitempty"`
						Arguments string `json:"arguments,omitempty"`
					} `json:"function,omitempty"`
				} `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage,omitempty"`
	}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}

		if !started {
			started = true
			writeSSE(w, map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":      chunk.ID,
					"type":    "message",
					"role":    "assistant",
					"model":   model,
					"content": []interface{}{},
					"usage": map[string]interface{}{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			})
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		// Handle DeepSeek reasoning_content → Anthropic thinking block
		if delta.ReasoningContent != "" {
			if !thinkingBlockStarted {
				thinkingBlockStarted = true
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]interface{}{
						"type":     "thinking",
						"thinking": "",
					},
				})
			}
			writeSSE(w, map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]interface{}{
					"type":     "thinking_delta",
					"thinking": delta.ReasoningContent,
				},
			})
		}

		if delta.Content != "" {
			// Close thinking block before starting text block
			if thinkingBlockStarted && !textBlockStarted {
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++
				thinkingBlockStarted = false
			}
			if !textBlockStarted {
				textBlockStarted = true
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]interface{}{
						"type": "text",
						"text": "",
					},
				})
			}
			writeSSE(w, map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": delta.Content,
				},
			})
		}

		for _, tc := range delta.ToolCalls {
			hasToolCall = true
			for tc.Index >= len(pendingToolCalls) {
				pendingToolCalls = append(pendingToolCalls, toolCallAccum{})
			}
			if tc.ID != "" {
				pendingToolCalls[tc.Index].ID = tc.ID
			}
			if tc.Function.Name != "" {
				pendingToolCalls[tc.Index].Name = tc.Function.Name
			}
			pendingToolCalls[tc.Index].Arguments += tc.Function.Arguments
		}

		if chunk.Choices[0].FinishReason != nil {
			finishReason := *chunk.Choices[0].FinishReason

			if thinkingBlockStarted {
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++
			}

			if textBlockStarted {
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++
			}

			for _, tc := range pendingToolCalls {
				var inputParsed interface{}
				if json.Unmarshal([]byte(tc.Arguments), &inputParsed) != nil {
					inputParsed = map[string]interface{}{}
				}

				writeSSE(w, map[string]interface{}{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": map[string]interface{}{},
					},
				})

				argsJSON, _ := json.Marshal(inputParsed)
				writeSSE(w, map[string]interface{}{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": string(argsJSON),
					},
				})

				writeSSE(w, map[string]interface{}{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++
			}

			stopReason := "end_turn"
			switch finishReason {
			case "length":
				stopReason = "max_tokens"
			case "tool_calls":
				stopReason = "tool_use"
			}

			writeSSE(w, map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason": stopReason,
				},
				"usage": map[string]interface{}{
					"output_tokens": outputTokens,
				},
			})

			writeSSE(w, map[string]interface{}{
				"type": "message_stop",
			})
		}
	}

	return inputTokens, outputTokens, hasToolCall
}

func ProxyDirect(baseURL string, apiKey string, body []byte) (*http.Response, error) {
	return ProxyDirectWithTimeout(baseURL, apiKey, body, 2*time.Minute)
}

func ProxyDirectWithTimeout(baseURL string, apiKey string, body []byte, timeout time.Duration) (*http.Response, error) {
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
		Timeout:   timeout,
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
	return orchestrator.LoadFullPrompt()
}

func loadMCPTools() ([]interface{}, error) {
	return orchestrator.LoadAllTools()
}

func getMaxTokensForModel(model string, requestedTokens int) int {
	modelLimits := map[string]int{
		"gpt-3.5-turbo":     4096,
		"gpt-4":             8192,
		"gpt-4-turbo":       16384,
		"gpt-4o":            16384,
		"gpt-4o-mini":       16384,
		"claude-3-haiku":    4096,
		"claude-3-sonnet":   4096,
		"claude-3-opus":     4096,
		"claude-3.5-sonnet": 8192,
		"claude-sonnet-4":   16384,
		"claude-sonnet-4.5": 16384,
		"claude-sonnet-4-5": 16384,
		"claude-opus-4":     32768,
		"claude-opus-4-6":   32768,
		"llama3-8b-8192":    8192,
		"llama3-70b-8192":   8192,
		"mixtral-8x7b-32768":              32768,
		"gemma-7b-it":                     8192,
		"moonshotai/kimi-k2-instruct-0905": 16384,
		"moonshot-v1-8k":                  8192,
		"moonshot-v1-32k":                 32768,
		"moonshot-v1-128k":                128000,
		"deepseek-chat":                   8192,
		"deepseek-reasoner":               64000,
	}

	defaultLimit := 8192
	maxSafeLimit := 128000

	modelLimit, exists := modelLimits[model]
	if !exists {
		modelLimit = defaultLimit
	}

	if requestedTokens <= 0 {
		return modelLimit
	}

	if requestedTokens > maxSafeLimit {
		return maxSafeLimit
	}
	if requestedTokens > modelLimit {
		return modelLimit
	}
	return requestedTokens
}

func loadMCPToolsForOpenAI() ([]interface{}, error) {
	tools, err := loadMCPTools()
	if err != nil {
		return nil, err
	}

	var openaiTools []interface{}
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			openaiTool := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        toolMap["name"],
					"description": toolMap["description"],
					"parameters":  toolMap["input_schema"],
				},
			}
			openaiTools = append(openaiTools, openaiTool)
		}
	}

	return openaiTools, nil
}

func loadMCPToolsForAnthropic() ([]interface{}, error) {
	return loadMCPTools()
}

func IsClaudeCodeRequest(bodyBytes []byte) bool {
	var check struct {
		System []interface{} `json:"system"`
		Tools  []interface{} `json:"tools"`
	}
	if err := json.Unmarshal(bodyBytes, &check); err != nil {
		return false
	}
	return len(check.System) > 0 && len(check.Tools) > 0
}

func InjectSystemMessage(bodyBytes []byte, model string) []byte {
	if len(bodyBytes) == 0 {
		return bodyBytes
	}

	if IsClaudeCodeRequest(bodyBytes) {
		return passClaudeCodeRequest(bodyBytes, model)
	}

	var req AnthropicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return bodyBytes
	}

	req.Model = model
	req.MaxTokens = getMaxTokensForModel(model, req.MaxTokens)

	systemContent := ""
	if req.System != nil {
		systemContent = extractSystemText(req.System)
	}

	customSystemMsg, err := loadCustomSystemMessage()
	if err == nil && customSystemMsg != "" {
		if systemContent != "" {
			systemContent = customSystemMsg + "\n\n" + systemContent
		} else {
			systemContent = customSystemMsg
		}
	}

	if systemContent != "" {
		systemJSON, err := json.Marshal(systemContent)
		if err == nil {
			req.System = json.RawMessage(systemJSON)
		}
	}

	tools, err := loadMCPToolsForAnthropic()
	if err == nil && len(tools) > 0 {
		req.Tools = tools
	}

	modified, _ := json.Marshal(req)
	return modified
}

func passClaudeCodeRequest(bodyBytes []byte, model string) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return bodyBytes
	}

	modelJSON, _ := json.Marshal(model)
	raw["model"] = json.RawMessage(modelJSON)

	modified, err := json.Marshal(raw)
	if err != nil {
		return bodyBytes
	}
	return modified
}

func InjectSystemMessageOrchestrated(bodyBytes []byte, model string, intent string, planResult *orchestrator.PlanResult) []byte {
	if len(bodyBytes) == 0 {
		return bodyBytes
	}

	if IsClaudeCodeRequest(bodyBytes) {
		return passClaudeCodeRequest(bodyBytes, model)
	}

	var req AnthropicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return bodyBytes
	}

	req.Model = model
	req.MaxTokens = getMaxTokensForModel(model, req.MaxTokens)

	modified, err := orchestrator.New(nil).BuildExecuteRequest(mustMarshal(req), intent, planResult, model)
	if err != nil {
		return InjectSystemMessage(bodyBytes, model)
	}

	var result AnthropicRequest
	if err := json.Unmarshal(modified, &result); err != nil {
		return InjectSystemMessage(bodyBytes, model)
	}

	result.Model = model
	result.MaxTokens = getMaxTokensForModel(model, req.MaxTokens)

	out, _ := json.Marshal(result)
	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
