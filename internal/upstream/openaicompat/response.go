package openaicompat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OpenAI Chat Completion Response structure
type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// StreamChunk represents an SSE chunk in streaming responses
type StreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string          `json:"role,omitempty"`
			Content   string          `json:"content,omitempty"`
			ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// ExtractTokens extracts token usage from a non-streaming response
func ExtractTokens(body []byte) (int, int, error) {
	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, err
	}

	return resp.Usage.PromptTokens, resp.Usage.CompletionTokens, nil
}

// DetectToolCall checks if a non-streaming OpenAI response contains tool calls.
func DetectToolCall(body []byte) bool {
	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	if len(resp.Choices) > 0 {
		if resp.Choices[0].FinishReason == "tool_calls" {
			return true
		}
		if len(resp.Choices[0].Message.ToolCalls) > 2 { // not empty JSON array "[]"
			return true
		}
	}
	return false
}

// StreamTransform passes through the OpenAI SSE stream, capturing usage tokens.
// Returns input tokens, output tokens, and whether a tool call was detected.
func StreamTransform(r io.Reader, w io.Writer) (int, int, bool) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size to 1MB to handle large SSE chunks
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))
	inputTokens, outputTokens := 0, 0
	hasToolCall := false

	for scanner.Scan() {
		line := scanner.Text()

		// Pass through all lines
		fmt.Fprintf(w, "%s\n", line)

		// Extract usage from data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Skip [DONE] marker
			if strings.TrimSpace(data) == "[DONE]" {
				continue
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if chunk.Usage != nil {
					inputTokens = chunk.Usage.PromptTokens
					outputTokens = chunk.Usage.CompletionTokens
				}
				// Detect tool calls in streaming chunks
				if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason == "tool_calls" {
					hasToolCall = true
				}
			}
		}
	}

	return inputTokens, outputTokens, hasToolCall
}
