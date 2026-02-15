package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Google Response structures
type GoogleResponse struct {
	Candidates []GoogleCandidate `json:"candidates"`
	Usage      GoogleUsage       `json:"usageMetadata"`
}

type GoogleCandidate struct {
	Content GoogleContent `json:"content"`
}

type GoogleUsage struct {
	PromptTokens     int `json:"promptTokenCount"`
	CandidatesTokens int `json:"candidatesTokenCount"`
	TotalTokens      int `json:"totalTokenCount"`
}

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

// TransformResponse converts a non-streaming Google response to Anthropic format
func TransformResponse(googleBody []byte, model string) ([]byte, int, int, error) {
	var gResp GoogleResponse
	if err := json.Unmarshal(googleBody, &gResp); err != nil {
		return nil, 0, 0, err
	}

	text := ""
	if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
		text = gResp.Candidates[0].Content.Parts[0].Text
	}

	anthropicResp := map[string]interface{}{
		"id":    fmt.Sprintf("antigravity-%d", time.Now().Unix()),
		"type":  "message",
		"role":  "assistant",
		"model": model,
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"usage": map[string]int{
			"input_tokens":  gResp.Usage.PromptTokens,
			"output_tokens": gResp.Usage.CandidatesTokens,
		},
	}

	res, err := json.Marshal(anthropicResp)
	return res, gResp.Usage.PromptTokens, gResp.Usage.CandidatesTokens, err
}

// StreamTransform reads Google's SSE stream and writes Anthropic SSE events
func StreamTransform(r io.Reader, w io.Writer) (int, int) {
	scanner := bufio.NewScanner(r)
	inputTokens, outputTokens := 0, 0

	fmt.Fprintf(w, "event: message_start\ndata: {\"type\": \"message_start\", \"message\": {\"role\": \"assistant\", \"content\": []}}\n\n")

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var gResp GoogleResponse
		if err := json.Unmarshal([]byte(data), &gResp); err != nil {
			continue
		}

		// Update usage if available
		if gResp.Usage.TotalTokens > 0 {
			inputTokens = gResp.Usage.PromptTokens
			outputTokens = gResp.Usage.CandidatesTokens
		}

		if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
			text := gResp.Candidates[0].Content.Parts[0].Text
			event := AnthropicEvent{
				Type: "content_block_delta",
				Delta: &AnthropicDelta{
					Text: text,
				},
			}
			eb, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", string(eb))
		}
	}

	// Final usage and end events
	usageEvent := AnthropicEvent{
		Type: "message_delta",
		Usage: &AnthropicUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}
	ub, _ := json.Marshal(usageEvent)
	fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", string(ub))
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n")

	return inputTokens, outputTokens
}
