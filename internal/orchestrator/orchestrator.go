package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Orchestrator struct {
	logger *log.Logger
}

func New(logger *log.Logger) *Orchestrator {
	return &Orchestrator{logger: logger}
}

type ClassifyResult struct {
	Intent    string `json:"intent"`
	Reasoning string `json:"reasoning"`
}

type PlanResult struct {
	Steps       []string `json:"steps"`
	ToolsNeeded []string `json:"tools_needed"`
}

type PhaseRequest struct {
	BaseURL  string
	APIKey   string
	Model    string
	Messages []map[string]interface{}
}

func (o *Orchestrator) Classify(pr PhaseRequest) (*ClassifyResult, error) {
	group, err := GetGroupForIntent("classify")
	if err != nil {
		return nil, fmt.Errorf("failed to load classify group: %w", err)
	}

	systemPrompt, err := LoadPromptSections(group.PromptSections)
	if err != nil {
		return nil, fmt.Errorf("failed to load classify prompt: %w", err)
	}

	// Wrap user messages so the model classifies rather than answers them
	wrappedMessages := wrapMessagesForClassification(pr.Messages)

	body := map[string]interface{}{
		"model":      pr.Model,
		"max_tokens": 512,
		"stream":     false,
		"system":     systemPrompt,
		"messages":   wrappedMessages,
	}

	respBytes, err := o.callAPI(pr.BaseURL, pr.APIKey, body)
	if err != nil {
		return nil, fmt.Errorf("classify API call failed: %w", err)
	}

	text := extractResponseText(respBytes)
	var result ClassifyResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		// Try to extract JSON from prose-wrapped responses
		extracted := tryExtractJSON(text)
		if err2 := json.Unmarshal([]byte(extracted), &result); err2 != nil {
			o.logger.Printf("[orchestrator/classify] failed to parse response: %s, raw: %s", err, text)
			return &ClassifyResult{Intent: "full", Reasoning: "failed to parse classification"}, nil
		}
	}

	o.logger.Printf("[orchestrator/classify] intent=%s reasoning=%s", result.Intent, result.Reasoning)
	return &result, nil
}

func (o *Orchestrator) Plan(pr PhaseRequest, intent string) (*PlanResult, error) {
	group, err := GetGroupForIntent("plan")
	if err != nil {
		return nil, fmt.Errorf("failed to load plan group: %w", err)
	}

	systemPrompt, err := LoadPromptSections(group.PromptSections)
	if err != nil {
		return nil, fmt.Errorf("failed to load plan prompt: %w", err)
	}

	systemPrompt = systemPrompt + "\n\nThe classified intent is: " + intent

	wrappedMessages := wrapMessagesForPlanning(pr.Messages, intent)

	body := map[string]interface{}{
		"model":      pr.Model,
		"max_tokens": 1024,
		"stream":     false,
		"system":     systemPrompt,
		"messages":   wrappedMessages,
	}

	respBytes, err := o.callAPI(pr.BaseURL, pr.APIKey, body)
	if err != nil {
		return nil, fmt.Errorf("plan API call failed: %w", err)
	}

	text := extractResponseText(respBytes)
	var result PlanResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		extracted := tryExtractJSON(text)
		if err2 := json.Unmarshal([]byte(extracted), &result); err2 != nil {
			o.logger.Printf("[orchestrator/plan] failed to parse response: %s, raw: %s", err, text)
			return nil, nil
		}
	}

	o.logger.Printf("[orchestrator/plan] steps=%d tools_needed=%v", len(result.Steps), result.ToolsNeeded)
	return &result, nil
}

func (o *Orchestrator) BuildExecuteRequest(originalBody []byte, intent string, planResult *PlanResult) ([]byte, error) {
	group, err := GetGroupForIntent(intent)
	if err != nil {
		return nil, fmt.Errorf("failed to get group for intent %s: %w", intent, err)
	}

	systemPrompt, err := LoadPromptSections(group.PromptSections)
	if err != nil {
		return nil, fmt.Errorf("failed to load prompt sections: %w", err)
	}

	toolNames := group.Tools
	if planResult != nil && len(planResult.ToolsNeeded) > 0 {
		toolNames = mergeToolNames(toolNames, planResult.ToolsNeeded)
	}

	tools, err := LoadToolsByNames(toolNames)
	if err != nil {
		return nil, fmt.Errorf("failed to load tools: %w", err)
	}

	var req map[string]json.RawMessage
	if err := json.Unmarshal(originalBody, &req); err != nil {
		return nil, err
	}

	systemJSON, _ := json.Marshal(systemPrompt)
	req["system"] = systemJSON

	if len(tools) > 0 {
		toolsJSON, _ := json.Marshal(tools)
		req["tools"] = toolsJSON
	} else {
		delete(req, "tools")
	}

	return json.Marshal(req)
}

func (o *Orchestrator) callAPI(baseURL string, apiKey string, body map[string]interface{}) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	apiURL := strings.TrimRight(baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

func extractResponseText(respBytes []byte) string {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return ""
	}
	for _, c := range resp.Content {
		if c.Type == "text" {
			return stripCodeFences(strings.TrimSpace(c.Text))
		}
	}
	return ""
}

// stripCodeFences removes markdown code fences (```json ... ```) that LLMs
// sometimes wrap around JSON responses.
func stripCodeFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence line (e.g. "```json\n" or "```\n")
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	}
	// Remove closing fence
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// tryExtractJSON attempts to find a JSON object in text where the model may have
// included surrounding prose. Returns the original text if no JSON object is found.
func tryExtractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}
	end := strings.LastIndex(s, "}")
	if end == -1 || end <= start {
		// Try to repair truncated JSON
		return tryRepairTruncatedJSON(s[start:])
	}
	candidate := s[start : end+1]
	if json.Valid([]byte(candidate)) {
		return candidate
	}
	return s
}

// tryRepairTruncatedJSON attempts to fix JSON truncated by max_tokens.
// Handles cases like: {"intent": "question", "reasoning": "The user is asking...
func tryRepairTruncatedJSON(s string) string {
	if !strings.HasPrefix(s, "{") {
		return s
	}
	// Try progressively simpler repairs
	// 1. Close an open string value and close the object
	repaired := s
	// Trim trailing incomplete escape sequences
	repaired = strings.TrimRight(repaired, "\\")
	// If we're inside a string value, close it
	// Count unescaped quotes to determine if we're inside a string
	inString := false
	for i := 0; i < len(repaired); i++ {
		if repaired[i] == '\\' {
			i++ // skip escaped char
			continue
		}
		if repaired[i] == '"' {
			inString = !inString
		}
	}
	if inString {
		repaired += `"}`
	} else {
		repaired += "}"
	}
	if json.Valid([]byte(repaired)) {
		return repaired
	}
	// 2. Try closing with array bracket too (for plan responses)
	if inString {
		repaired = s + `"]}`
	} else {
		repaired = s + "]}"
	}
	if json.Valid([]byte(repaired)) {
		return repaired
	}
	// 3. Handle trailing comma after a complete value (e.g. {"steps": [...],)
	trimmed := strings.TrimRight(s, ", \t\n")
	if json.Valid([]byte(trimmed + "}")) {
		return trimmed + "}"
	}
	return s
}

// wrapMessagesForClassification wraps user messages so the model classifies
// rather than answering them directly.
func wrapMessagesForClassification(messages []map[string]interface{}) []map[string]interface{} {
	userContent := extractUserContent(messages)
	return []map[string]interface{}{
		{
			"role":    "user",
			"content": "Classify the intent of the following user message. Respond with ONLY a JSON object.\n\nUser message:\n\"\"\"\n" + userContent + "\n\"\"\"",
		},
	}
}

// wrapMessagesForPlanning wraps user messages so the model plans rather than
// answering them directly.
func wrapMessagesForPlanning(messages []map[string]interface{}, intent string) []map[string]interface{} {
	userContent := extractUserContent(messages)
	return []map[string]interface{}{
		{
			"role":    "user",
			"content": "Create an execution plan for the following user request (classified as \"" + intent + "\"). Respond with ONLY a JSON object.\n\nUser request:\n\"\"\"\n" + userContent + "\n\"\"\"",
		},
	}
}

// extractUserContent gets the text content from user messages.
func extractUserContent(messages []map[string]interface{}) string {
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role == "user" {
			if content, ok := msg["content"].(string); ok {
				return content
			}
			// Handle content as array of blocks (Anthropic format)
			if blocks, ok := msg["content"].([]interface{}); ok {
				var parts []string
				for _, block := range blocks {
					if b, ok := block.(map[string]interface{}); ok {
						if text, ok := b["text"].(string); ok {
							parts = append(parts, text)
						}
					}
				}
				return strings.Join(parts, "\n")
			}
		}
	}
	return ""
}

func mergeToolNames(base []string, additional []string) []string {
	nameSet := make(map[string]bool)
	for _, n := range base {
		nameSet[n] = true
	}
	for _, n := range additional {
		if n == "*" {
			return []string{"*"}
		}
		nameSet[n] = true
	}
	var merged []string
	for name := range nameSet {
		merged = append(merged, name)
	}
	return merged
}
