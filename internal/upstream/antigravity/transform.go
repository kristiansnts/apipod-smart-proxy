package antigravity

import (
	"encoding/json"
	"time"
)

type AnthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func TransformResponse(body []byte, model string) ([]byte, int, int, error) {
	var aResp AnthropicResponse
	if err := json.Unmarshal(body, &aResp); err != nil {
		return body, 0, 0, nil
	}

	text := ""
	if len(aResp.Content) > 0 {
		text = aResp.Content[0].Text
	}

	oResp := OpenAIResponse{
		ID:      "chatcmpl-" + aResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
	}

	oResp.Choices = append(oResp.Choices, struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
	}{
		Message: struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			Role:    "assistant",
			Content: text,
		},
		FinishReason: "stop",
		Index:        0,
	})

	oResp.Usage.PromptTokens = aResp.Usage.InputTokens
	oResp.Usage.CompletionTokens = aResp.Usage.OutputTokens
	oResp.Usage.TotalTokens = aResp.Usage.InputTokens + aResp.Usage.OutputTokens

	res, err := json.Marshal(oResp)
	return res, oResp.Usage.PromptTokens, oResp.Usage.CompletionTokens, err
}

func StreamTransform(r interface{}, w interface{}) (int, int) {
	// Stub for now to avoid compilation errors, real streaming to be added next
	return 0, 0
}
