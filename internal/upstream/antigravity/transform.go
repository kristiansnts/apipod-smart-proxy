package antigravity

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicRequest represents the incoming Claude-style payload
type AnthropicRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GoogleRequest represents the payload for Vertex AI / Cloud Code
type GoogleRequest struct {
	Contents         []GoogleContent  `json:"contents"`
	GenerationConfig GenerationConfig `json:"generationConfig"`
}

type GoogleContent struct {
	Role  string       `json:"role"`
	Parts []GooglePart `json:"parts"`
}

type GooglePart struct {
	Text string `json:"text"`
}

type GenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	CandidateCount  int     `json:"candidateCount,omitempty"`
}

// TransformAnthropicToGoogle performs the heavy lifting of converting the protocol
func TransformAnthropicToGoogle(anthropicBody []byte) ([]byte, error) {
	var aReq AnthropicRequest
	if err := json.Unmarshal(anthropicBody, &aReq); err != nil {
		return nil, fmt.Errorf("failed to unmarshal anthropic request: %w", err)
	}

	var gContents []GoogleContent

	// Handle System Prompt: Google Vertex requires system instructions as part of the messages 
	// or in a separate field depending on the model. To be safe like Badri, we prepend it 
	// to the first user message if it exists.
	systemPrompt := aReq.System

	for i, msg := range aReq.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		content := msg.Content
		if i == 0 && systemPrompt != "" && msg.Role == "user" {
			content = fmt.Sprintf("%s\n\n%s", systemPrompt, content)
		}

		gContents = append(gContents, GoogleContent{
			Role: role,
			Parts: []GooglePart{{Text: content}},
		})
	}

	gReq := GoogleRequest{
		Contents: gContents,
		GenerationConfig: GenerationConfig{
			MaxOutputTokens: aReq.MaxTokens,
			Temperature:     aReq.Temperature,
			CandidateCount:  1,
		},
	}

	// Default reasonable values if missing
	if gReq.GenerationConfig.MaxOutputTokens == 0 {
		gReq.GenerationConfig.MaxOutputTokens = 4096
	}

	return json.Marshal(gReq)
}
