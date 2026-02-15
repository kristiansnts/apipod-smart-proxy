package antigravity

import (
	"encoding/json"
)

// Anthropic Request
type AnthropicRequest struct {
	Model    string             `json:"model"`
	Messages []AnthropicMessage `json:"messages"`
	System   string             `json:"system,omitempty"`
	Stream   bool               `json:"stream,omitempty"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Google Request (simplified Cloud Code style)
type GoogleRequest struct {
	Contents []GoogleContent `json:"contents"`
	SystemInstruction *GoogleContent `json:"system_instruction,omitempty"`
}

type GoogleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GooglePart `json:"parts"`
}

type GooglePart struct {
	Text string `json:"text"`
}

func TransformAnthropicToGoogle(anthropicBody []byte) ([]byte, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(anthropicBody, &req); err != nil {
		return nil, err
	}

	googleReq := GoogleRequest{
		Contents: []GoogleContent{},
	}

	if req.System != "" {
		googleReq.SystemInstruction = &GoogleContent{
			Parts: []GooglePart{{Text: req.System}},
		}
	}

	for _, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		googleReq.Contents = append(googleReq.Contents, GoogleContent{
			Role:  role,
			Parts: []GooglePart{{Text: m.Content}},
		})
	}

	return json.Marshal(googleReq)
}
