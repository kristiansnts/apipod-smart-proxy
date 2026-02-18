package googleaistudio

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- OpenAI request/response types (subset) ---

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role       string              `json:"role"`
	Content    json.RawMessage     `json:"content"` // string or array
	ToolCalls  []openAIToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	ExtraContent map[string]interface{} `json:"extra_content,omitempty"`
}

// --- Gemini types ---

type geminiRequest struct {
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiContent           `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
	Tools             []geminiTool             `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// --- Gemini response types ---

type geminiResponse struct {
	Candidates    []geminiCandidate  `json:"candidates"`
	UsageMetadata *geminiUsage       `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent `json:"content"`
	FinishReason  string        `json:"finishReason,omitempty"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// cleanSchema removes fields from a JSON Schema that Google AI Studio does not support,
// such as "$schema" and "additionalProperties". It operates recursively on nested schemas.
func cleanSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}

	// Remove unsupported top-level fields
	delete(m, "$schema")
	delete(m, "additionalProperties")
	delete(m, "exclusiveMinimum")
	delete(m, "exclusiveMaximum")
	delete(m, "propertyNames")

	// Recurse into "properties" values
	if props, ok := m["properties"]; ok {
		var propMap map[string]json.RawMessage
		if json.Unmarshal(props, &propMap) == nil {
			for k, v := range propMap {
				propMap[k] = cleanSchema(v)
			}
			if b, err := json.Marshal(propMap); err == nil {
				m["properties"] = b
			}
		}
	}

	// Recurse into "items"
	if items, ok := m["items"]; ok {
		m["items"] = cleanSchema(items)
	}

	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// getTextContent extracts plain text from OpenAI content which can be a string or array.
func getTextContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Array of content parts (vision messages, etc.)
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				return p.Text
			}
		}
	}
	return string(raw)
}

// OpenAIToGemini converts an OpenAI chat completion request to Gemini format.
// Returns the Gemini request body, the model name, whether streaming is requested, and any error.
func OpenAIToGemini(body []byte) ([]byte, string, bool, error) {
	var req openAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "", false, err
	}

	gemReq := geminiRequest{}

	// Generation config
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil {
		gemReq.GenerationConfig = &geminiGenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	// Convert tools
	if len(req.Tools) > 0 {
		var decls []geminiFunctionDecl
		for _, t := range req.Tools {
			if t.Type == "function" {
				decls = append(decls, geminiFunctionDecl{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  cleanSchema(t.Function.Parameters),
				})
			}
		}
		if len(decls) > 0 {
			gemReq.Tools = []geminiTool{{FunctionDeclarations: decls}}
		}
	}

	// Convert messages
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			gemReq.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: getTextContent(msg.Content)}},
			}

		case "user":
			gemReq.Contents = append(gemReq.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: getTextContent(msg.Content)}},
			})

		case "assistant":
			var parts []geminiPart
			if len(msg.ToolCalls) > 0 {
				// Check if any tool call has a thoughtSignature
				hasAnySignature := false
				for _, tc := range msg.ToolCalls {
					if tc.ExtraContent != nil {
						if googleData, ok := tc.ExtraContent["google"].(map[string]interface{}); ok {
							if sig, ok := googleData["thought_signature"].(string); ok && sig != "" {
								hasAnySignature = true
								break
							}
						}
					}
				}

				if hasAnySignature {
					// Tool calls originated from Gemini — use native functionCall parts with signatures
					for _, tc := range msg.ToolCalls {
						var args json.RawMessage
						if tc.Function.Arguments != "" {
							args = json.RawMessage(tc.Function.Arguments)
						}
						var thoughtSig string
						if tc.ExtraContent != nil {
							if googleData, ok := tc.ExtraContent["google"].(map[string]interface{}); ok {
								if sig, ok := googleData["thought_signature"].(string); ok {
									thoughtSig = sig
								}
							}
						}
						part := geminiPart{
							FunctionCall: &geminiFunctionCall{
								Name: tc.Function.Name,
								Args: args,
							},
						}
						if thoughtSig != "" {
							part.ThoughtSignature = thoughtSig
						}
						parts = append(parts, part)
					}
				} else {
					// Tool calls from non-Gemini clients — convert to text to avoid
					// thoughtSignature validation errors
					var textParts []string
					text := getTextContent(msg.Content)
					if text != "" {
						textParts = append(textParts, text)
					}
					for _, tc := range msg.ToolCalls {
						textParts = append(textParts, fmt.Sprintf("[Calling function %s(%s)]", tc.Function.Name, tc.Function.Arguments))
					}
					if len(textParts) > 0 {
						parts = append(parts, geminiPart{Text: strings.Join(textParts, "\n")})
					}
				}
			} else {
				text := getTextContent(msg.Content)
				if text != "" {
					parts = append(parts, geminiPart{Text: text})
				}
			}
			if len(parts) > 0 {
				gemReq.Contents = append(gemReq.Contents, geminiContent{
					Role:  "model",
					Parts: parts,
				})
			}

		case "tool":
			// Tool result
			contentStr := getTextContent(msg.Content)
			// Check if the corresponding function call had a thoughtSignature
			// by looking for the tool call in previous messages
			hasSig := hasThoughtSignatureForToolCall(req.Messages, msg.ToolCallID)
			if hasSig {
				// Use native functionResponse for Gemini-originated calls
				var respObj json.RawMessage
				var probe map[string]json.RawMessage
				if json.Unmarshal([]byte(contentStr), &probe) == nil {
					respObj = json.RawMessage(contentStr)
				} else {
					wrapped, _ := json.Marshal(map[string]string{"result": contentStr})
					respObj = json.RawMessage(wrapped)
				}
				gemReq.Contents = append(gemReq.Contents, geminiContent{
					Role: "user",
					Parts: []geminiPart{{
						FunctionResponse: &geminiFunctionResponse{
							Name:     msg.ToolCallID,
							Response: respObj,
						},
					}},
				})
			} else {
				// Convert to text for non-Gemini tool results
				toolName := msg.ToolCallID
				text := fmt.Sprintf("[Result of %s]: %s", toolName, contentStr)
				gemReq.Contents = append(gemReq.Contents, geminiContent{
					Role:  "user",
					Parts: []geminiPart{{Text: text}},
				})
			}
		}
	}

	// Merge consecutive contents with the same role (Gemini requires alternating roles)
	gemReq.Contents = mergeConsecutiveRoles(gemReq.Contents)

	out, err := json.Marshal(gemReq)
	return out, req.Model, req.Stream, err
}

// mergeConsecutiveRoles merges consecutive content entries with the same role.
func mergeConsecutiveRoles(contents []geminiContent) []geminiContent {
	if len(contents) <= 1 {
		return contents
	}
	merged := []geminiContent{contents[0]}
	for i := 1; i < len(contents); i++ {
		last := &merged[len(merged)-1]
		if contents[i].Role == last.Role {
			last.Parts = append(last.Parts, contents[i].Parts...)
		} else {
			merged = append(merged, contents[i])
		}
	}
	return merged
}

// hasThoughtSignatureForToolCall checks if the assistant message containing the given
// tool call ID has a thoughtSignature (i.e., it originated from Gemini).
func hasThoughtSignatureForToolCall(messages []openAIMessage, toolCallID string) bool {
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID == toolCallID {
				if tc.ExtraContent != nil {
					if googleData, ok := tc.ExtraContent["google"].(map[string]interface{}); ok {
						if sig, ok := googleData["thought_signature"].(string); ok && sig != "" {
							return true
						}
					}
				}
				return false
			}
		}
	}
	return false
}

// GeminiToOpenAI converts a Gemini response to OpenAI chat completion format.
func GeminiToOpenAI(body []byte, model string) ([]byte, int, int, bool, error) {
	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, 0, false, err
	}

	inputTokens, outputTokens := 0, 0
	if resp.UsageMetadata != nil {
		inputTokens = resp.UsageMetadata.PromptTokenCount
		outputTokens = resp.UsageMetadata.CandidatesTokenCount
	}

	hasToolCall := false
	content := ""
	var toolCalls []openAIToolCall
	finishReason := "stop"

	if len(resp.Candidates) > 0 {
		cand := resp.Candidates[0]
		for _, part := range cand.Content.Parts {
			if part.FunctionCall != nil {
				hasToolCall = true
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCall := openAIToolCall{
					ID:   "call_" + part.FunctionCall.Name,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsBytes),
					},
				}
				// Include thoughtSignature if present
				if part.ThoughtSignature != "" {
					toolCall.ExtraContent = map[string]interface{}{
						"google": map[string]interface{}{
							"thought_signature": part.ThoughtSignature,
						},
					}
				}
				toolCalls = append(toolCalls, toolCall)
			} else if part.Text != "" {
				content += part.Text
			}
		}

		switch cand.FinishReason {
		case "MAX_TOKENS":
			finishReason = "length"
		case "STOP":
			finishReason = "stop"
		}
		if hasToolCall {
			finishReason = "tool_calls"
		}
	}

	openaiResp := map[string]interface{}{
		"id":      "chatcmpl-gemini",
		"object":  "chat.completion",
		"created": 0,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": func() map[string]interface{} {
					m := map[string]interface{}{
						"role":    "assistant",
						"content": content,
					}
					if len(toolCalls) > 0 {
						m["tool_calls"] = toolCalls
					}
					return m
				}(),
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		},
	}

	out, err := json.Marshal(openaiResp)
	return out, inputTokens, outputTokens, hasToolCall, err
}
