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
			Role    string `json:"role"`
			Content string `json:"content"`
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
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
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

// StreamTransform passes through the OpenAI SSE stream, capturing usage tokens
func StreamTransform(r io.Reader, w io.Writer) (int, int) {
	scanner := bufio.NewScanner(r)
	inputTokens, outputTokens := 0, 0

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
			if err := json.Unmarshal([]byte(data), &chunk); err == nil && chunk.Usage != nil {
				inputTokens = chunk.Usage.PromptTokens
				outputTokens = chunk.Usage.CompletionTokens
			}
		}
	}

	return inputTokens, outputTokens
}
