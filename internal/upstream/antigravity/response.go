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

// AnthropicResponse represents the Anthropic Messages API response from the Rust engine
type AnthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// TransformResponse passes through the Anthropic response from the Rust engine,
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

// StreamTransform passes through the Anthropic SSE stream from the Rust engine,
// capturing usage tokens along the way.
func StreamTransform(r io.Reader, w io.Writer) (int, int) {
	scanner := bufio.NewScanner(r)
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
