package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Anthropic SSE structure
type AnthropicEvent struct {
	Type  string          `json:"type"`
	Delta *AnthropicDelta `json:"delta,omitempty"`
	Usage *AnthropicUsage `json:"usage,omitempty"`
}

type AnthropicDelta struct {
	Text string `json:"text"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicResponse represents the Anthropic Messages API response
type AnthropicResponse struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Model      string                   `json:"model"`
	Content    []map[string]interface{} `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// TransformResponse passes through the Anthropic response from the upstream,
// overriding the model name, and extracts token counts for usage logging.
func TransformResponse(body []byte, model string) ([]byte, int, int, error) {
	var resp AnthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, 0, err
	}

	inputTokens := resp.Usage.InputTokens
	outputTokens := resp.Usage.OutputTokens

	// Override model name to match the routed model
	resp.Model = model

	res, err := json.Marshal(resp)
	return res, inputTokens, outputTokens, err
}

// TransformResponseToOpenAI converts an Anthropic Messages response to OpenAI chat completions format,
// including tool_use blocks → tool_calls conversion.
func TransformResponseToOpenAI(body []byte, model string) ([]byte, int, int, bool, error) {
	var resp AnthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, 0, false, err
	}

	inputTokens := resp.Usage.InputTokens
	outputTokens := resp.Usage.OutputTokens
	hasToolCall := false

	// Build OpenAI response
	var textContent string
	var toolCalls []map[string]interface{}

	for _, block := range resp.Content {
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if text, ok := block["text"].(string); ok {
				textContent += text
			}
		case "tool_use":
			hasToolCall = true
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input := block["input"]
			argsBytes, _ := json.Marshal(input)

			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(argsBytes),
				},
			})
		}
	}

	// Map stop reason
	finishReason := "stop"
	switch resp.StopReason {
	case "tool_use":
		finishReason = "tool_calls"
	case "max_tokens":
		finishReason = "length"
	case "end_turn":
		finishReason = "stop"
	}

	message := map[string]interface{}{
		"role": "assistant",
	}
	if textContent != "" {
		message["content"] = textContent
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	openAIResp := map[string]interface{}{
		"id":      resp.ID,
		"object":  "chat.completion",
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		},
	}

	out, err := json.Marshal(openAIResp)
	return out, inputTokens, outputTokens, hasToolCall, err
}

// StreamTransform passes through the Anthropic SSE stream from the upstream,
// capturing usage tokens along the way.
func StreamTransform(r io.Reader, w io.Writer) (int, int) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size to 1MB to handle large SSE lines (e.g. tool calls)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))

	inputTokens, outputTokens := 0, 0

	for scanner.Scan() {
		line := scanner.Text()

		// Pass through all lines (event: lines, data: lines, empty lines)
		fmt.Fprintf(w, "%s\n", line)

		// Extract usage from data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event AnthropicEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil && event.Usage != nil {
				inputTokens = event.Usage.InputTokens
				outputTokens = event.Usage.OutputTokens
			}
		}
	}

	return inputTokens, outputTokens
}

// StreamTransformToOpenAI converts an Anthropic SSE stream to OpenAI SSE stream format,
// including tool_use blocks → tool_calls deltas.
func StreamTransformToOpenAI(r io.Reader, w io.Writer, model string) (int, int, bool) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))

	inputTokens, outputTokens := 0, 0
	hasToolCall := false
	sentFirstChunk := false
	messageID := ""
	toolCallIndex := 0

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}

		var event map[string]interface{}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				messageID, _ = msg["id"].(string)
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if v, ok := usage["input_tokens"].(float64); ok {
						inputTokens = int(v)
					}
				}
			}
			// Send initial role chunk
			if !sentFirstChunk {
				sentFirstChunk = true
				writeOpenAISSE(w, messageID, model, map[string]interface{}{
					"role":    "assistant",
					"content": "",
				}, nil)
			}

		case "content_block_start":
			if cb, ok := event["content_block"].(map[string]interface{}); ok {
				cbType, _ := cb["type"].(string)
				if cbType == "tool_use" {
					hasToolCall = true
					id, _ := cb["id"].(string)
					name, _ := cb["name"].(string)
					writeOpenAISSE(w, messageID, model, nil, []map[string]interface{}{
						{
							"index": toolCallIndex,
							"id":    id,
							"type":  "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": "",
							},
						},
					})
				}
			}

		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				deltaType, _ := delta["type"].(string)
				switch deltaType {
				case "text_delta":
					text, _ := delta["text"].(string)
					writeOpenAISSE(w, messageID, model, map[string]interface{}{
						"content": text,
					}, nil)
				case "input_json_delta":
					partialJSON, _ := delta["partial_json"].(string)
					writeOpenAISSE(w, messageID, model, nil, []map[string]interface{}{
						{
							"index": toolCallIndex,
							"function": map[string]interface{}{
								"arguments": partialJSON,
							},
						},
					})
				}
			}

		case "content_block_stop":
			// If this was a tool_use block, increment index for next one
			if hasToolCall {
				toolCallIndex++
			}

		case "message_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				stopReason, _ := delta["stop_reason"].(string)
				finishReason := "stop"
				switch stopReason {
				case "tool_use":
					finishReason = "tool_calls"
				case "max_tokens":
					finishReason = "length"
				}
				writeOpenAIFinish(w, messageID, model, finishReason)
			}
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if v, ok := usage["output_tokens"].(float64); ok {
					outputTokens = int(v)
				}
			}

		case "message_stop":
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(interface{ Flush() }); ok {
				f.Flush()
			}
		}
	}

	return inputTokens, outputTokens, hasToolCall
}

func writeOpenAISSE(w io.Writer, id string, model string, delta map[string]interface{}, toolCalls []map[string]interface{}) {
	chunk := map[string]interface{}{
		"id":     id,
		"object": "chat.completion.chunk",
		"model":  model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         buildDelta(delta, toolCalls),
				"finish_reason": nil,
			},
		},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", string(data))
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func writeOpenAIFinish(w io.Writer, id string, model string, finishReason string) {
	chunk := map[string]interface{}{
		"id":     id,
		"object": "chat.completion.chunk",
		"model":  model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finishReason,
			},
		},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", string(data))
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func buildDelta(content map[string]interface{}, toolCalls []map[string]interface{}) map[string]interface{} {
	d := map[string]interface{}{}
	if content != nil {
		for k, v := range content {
			d[k] = v
		}
	}
	if len(toolCalls) > 0 {
		d["tool_calls"] = toolCalls
	}
	return d
}
