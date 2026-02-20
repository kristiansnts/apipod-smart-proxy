package proxy

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/tools"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/anthropiccompat"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/openaicompat"
)

// AnthropicResponse represents a response from the Anthropic API
type AnthropicResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Model      string `json:"model"`
	Content    []struct {
		Type  string                 `json:"type"`
		Text  string                 `json:"text,omitempty"`
		ID    string                 `json:"id,omitempty"`
		Name  string                 `json:"name,omitempty"`
		Input map[string]interface{} `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// AnthropicRequest represents a request to the Anthropic API
type AnthropicRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	Messages  []map[string]interface{} `json:"messages"`
	System    string                   `json:"system,omitempty"`
	Tools     []interface{}            `json:"tools,omitempty"`
}

// handleToolExecution intercepts responses with tool_use and executes them
func (h *Handler) handleToolExecution(respBytes []byte, routing RoutingResult, originalRequest []byte) ([]byte, int, int, error) {
	const maxToolRounds = 15

	totalInputTokens := 0
	totalOutputTokens := 0

	// Parse original request to accumulate messages
	var currentReq AnthropicRequest
	if err := json.Unmarshal(originalRequest, &currentReq); err != nil {
		return respBytes, 0, 0, err
	}

	currentRespBytes := respBytes

	for round := 0; round < maxToolRounds; round++ {
		var response AnthropicResponse
		if err := json.Unmarshal(currentRespBytes, &response); err != nil {
			break
		}

		totalInputTokens += response.Usage.InputTokens
		totalOutputTokens += response.Usage.OutputTokens

		// Check if response contains tool_use
		var toolCalls []tools.ToolCall
		for _, content := range response.Content {
			if content.Type == "tool_use" {
				toolCalls = append(toolCalls, tools.ToolCall{
					ID:    content.ID,
					Name:  content.Name,
					Input: content.Input,
				})
			}
		}

		if len(toolCalls) == 0 {
			return currentRespBytes, totalInputTokens, totalOutputTokens, nil
		}

		h.runnerLogger.Printf("[tool_execution] round %d: executing %d tools", round+1, len(toolCalls))

		// Execute tools
		var toolResults []map[string]interface{}
		for i, call := range toolCalls {
			h.runnerLogger.Printf("[tool_execution] executing tool %d/%d: %s (id=%s)", i+1, len(toolCalls), call.Name, call.ID)
			result := h.toolExecutor.ExecuteTool(call)
			h.runnerLogger.Printf("[tool_execution] completed tool %d/%d: %s success=%v", i+1, len(toolCalls), call.Name, !result.IsError)

			toolResults = append(toolResults, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": result.ToolUseID,
				"content":     result.Content,
				"is_error":    result.IsError,
			})
		}

		// Add assistant message with tool_use
		assistantContent := []map[string]interface{}{}
		for _, content := range response.Content {
			assistantContent = append(assistantContent, map[string]interface{}{
				"type":  content.Type,
				"text":  content.Text,
				"id":    content.ID,
				"name":  content.Name,
				"input": content.Input,
			})
		}

		currentReq.Messages = append(currentReq.Messages, map[string]interface{}{
			"role":    "assistant",
			"content": assistantContent,
		})

		// Add tool results as user message
		currentReq.Messages = append(currentReq.Messages, map[string]interface{}{
			"role":    "user",
			"content": toolResults,
		})

		// Make follow-up request
		followupBytes, err := json.Marshal(currentReq)
		if err != nil {
			break
		}

		apiKey := h.resolveAPIKey(routing)
		timeouts := config.GetModelTimeouts(routing.Model)

		followupRespBytes, err := h.executeToolContinuationWithRetry(routing.BaseURL, apiKey, followupBytes, timeouts, routing.Model)
		if err != nil {
			h.runnerLogger.Printf("[tool_execution] follow-up request failed at round %d: %v", round+1, err)
			break
		}

		currentRespBytes = followupRespBytes
	}

	h.runnerLogger.Printf("[tool_execution] completed with %d input + %d output tokens", totalInputTokens, totalOutputTokens)
	return currentRespBytes, totalInputTokens, totalOutputTokens, nil
}

// executeToolContinuationWithRetry executes a tool continuation request with retry logic and exponential backoff
func (h *Handler) executeToolContinuationWithRetry(baseURL, apiKey string, requestBytes []byte, timeouts config.ModelTimeouts, model string) ([]byte, error) {
	var lastErr error
	
	for attempt := 0; attempt <= timeouts.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			delay := time.Duration(attempt) * timeouts.RetryDelay
			h.runnerLogger.Printf("[tool_execution] retry %d/%d for model=%s, waiting %v", attempt, timeouts.MaxRetries, model, delay)
			time.Sleep(delay)
		}

		h.runnerLogger.Printf("[tool_execution] attempting tool continuation (attempt %d/%d) for model=%s", attempt+1, timeouts.MaxRetries+1, model)
		
		// Create a custom ProxyDirect call with extended timeout
		followupResp, err := anthropiccompat.ProxyDirectWithTimeout(baseURL, apiKey, requestBytes, timeouts.ToolContinueTimeout)
		if err != nil {
			lastErr = err
			h.runnerLogger.Printf("[tool_execution] attempt %d failed for model=%s: %v", attempt+1, model, err)
			continue
		}
		defer followupResp.Body.Close()

		if followupResp.StatusCode >= 500 {
			// Server error - retry
			lastErr = err
			h.runnerLogger.Printf("[tool_execution] server error %d on attempt %d for model=%s", followupResp.StatusCode, attempt+1, model)
			continue
		} else if followupResp.StatusCode >= 400 {
			// Client error - don't retry
			respBody, _ := io.ReadAll(followupResp.Body)
			h.runnerLogger.Printf("[tool_execution] client error %d for model=%s: %s", followupResp.StatusCode, model, string(respBody))
			return respBody, err
		}

		// Success
		followupBytes, err := io.ReadAll(followupResp.Body)
		if err != nil {
			lastErr = err
			continue
		}
		
		h.runnerLogger.Printf("[tool_execution] successful continuation on attempt %d for model=%s", attempt+1, model)
		return followupBytes, nil
	}
	
	return nil, lastErr
}

// OpenAI-format structures for tool execution
type openAIToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIToolCallEntry struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolCallFunc `json:"function"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role      string                `json:"role"`
			Content   *string               `json:"content"`
			ToolCalls []openAIToolCallEntry  `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// handleToolExecutionOpenAI intercepts OpenAI-format responses with tool_calls,
// executes them locally, and sends a follow-up request through the OpenAI-compat endpoint.
// Returns the final Anthropic-format response bytes, input/output tokens, hasToolCall, and error.
func (h *Handler) handleToolExecutionOpenAI(openaiRespBytes []byte, routing RoutingResult, openaiRequestBytes []byte, model string, path string) ([]byte, int, int, bool, error) {
	const maxToolRounds = 15
	const maxThinkingRetries = 2
	const thinkingThreshold = 500 // chars of text that looks like reasoning about tools

	// Try to extract tool calls from text for models that don't emit structured tool_calls
	openaiRespBytes = anthropiccompat.ExtractToolCallsFromText(openaiRespBytes)

	totalInputTokens := 0
	totalOutputTokens := 0
	hasToolCall := false

	// Parse the current request to accumulate messages
	var currentReq map[string]interface{}
	if err := json.Unmarshal(openaiRequestBytes, &currentReq); err != nil {
		anthropicResp, in, out, tc, _, err2 := anthropiccompat.OpenAIResponseToAnthropic(openaiRespBytes, model)
		if err2 != nil {
			return openaiRespBytes, 0, 0, false, err
		}
		return anthropicResp, in, out, tc, nil
	}

	currentRespBytes := openaiRespBytes
	thinkingRetries := 0

	for round := 0; round < maxToolRounds; round++ {
		var response openAIChatResponse
		if err := json.Unmarshal(currentRespBytes, &response); err != nil {
			break
		}

		totalInputTokens += response.Usage.PromptTokens
		totalOutputTokens += response.Usage.CompletionTokens

		// Check if response contains tool_calls
		if len(response.Choices) == 0 || len(response.Choices[0].Message.ToolCalls) == 0 {
			// Check for thinking loop: long text about tools but no actual tool call
			content := ""
			if len(response.Choices) > 0 && response.Choices[0].Message.Content != nil {
				content = *response.Choices[0].Message.Content
			}

			if thinkingRetries < maxThinkingRetries && isThinkingLoop(content, thinkingThreshold) {
				thinkingRetries++
				h.runnerLogger.Printf("[tool_execution] detected thinking loop (retry %d/%d), nudging model=%s", thinkingRetries, maxThinkingRetries, model)

				// Add the thinking text as assistant, then nudge as user
				msgs, _ := currentReq["messages"].([]interface{})
				// Truncate the thinking to save tokens
				truncated := content
				if len(truncated) > 200 {
					truncated = truncated[:200] + "..."
				}
				msgs = append(msgs, map[string]interface{}{
					"role":    "assistant",
					"content": truncated,
				})
				msgs = append(msgs, map[string]interface{}{
					"role":    "user",
					"content": "Stop explaining and directly call the appropriate tool now. Use the tool_calls format.",
				})
				currentReq["messages"] = msgs

				followupBytes, err := json.Marshal(currentReq)
				if err != nil {
					break
				}

				apiKey := h.resolveAPIKey(routing)
				timeouts := config.GetModelTimeouts(routing.Model)

				followupRespBytes, err := h.executeToolContinuationOpenAIWithRetry(routing.BaseURL, apiKey, path, followupBytes, timeouts, model)
				if err != nil {
					h.runnerLogger.Printf("[tool_execution] thinking nudge failed: %v", err)
					break
				}

				followupRespBytes = anthropiccompat.ExtractToolCallsFromText(followupRespBytes)
				currentRespBytes = followupRespBytes
				continue
			}

			// No tool calls and not a thinking loop — done
			anthropicResp, _, _, tc, _, err := anthropiccompat.OpenAIResponseToAnthropic(currentRespBytes, model)
			if err != nil {
				return currentRespBytes, totalInputTokens, totalOutputTokens, hasToolCall, err
			}
			return anthropicResp, totalInputTokens, totalOutputTokens, hasToolCall || tc, nil
		}

		hasToolCall = true
		thinkingRetries = 0 // reset on successful tool call
		h.runnerLogger.Printf("[tool_execution] round %d: %d tool calls from model=%s", round+1, len(response.Choices[0].Message.ToolCalls), model)

		var toolCalls []tools.ToolCall
		for _, tc := range response.Choices[0].Message.ToolCalls {
			var input map[string]interface{}
			if json.Unmarshal([]byte(tc.Function.Arguments), &input) != nil {
				input = map[string]interface{}{}
			}
			toolCalls = append(toolCalls, tools.ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		h.runnerLogger.Printf("[tool_execution] executing %d tools", len(toolCalls))

		// Execute tools
		var toolResultMsgs []map[string]interface{}
		for _, call := range toolCalls {
			h.runnerLogger.Printf("[tools] executing %s with id %s", call.Name, call.ID)
			result := h.toolExecutor.ExecuteTool(call)
			h.runnerLogger.Printf("[tool_execution] executed %s: success=%v", call.Name, !result.IsError)

			toolResultMsgs = append(toolResultMsgs, map[string]interface{}{
				"role":         "tool",
				"content":      result.Content,
				"tool_call_id": result.ToolUseID,
			})
		}

		// Append assistant message with tool_calls + tool results to conversation
		msgs, _ := currentReq["messages"].([]interface{})

		assistantMsg := map[string]interface{}{
			"role":       "assistant",
			"tool_calls": response.Choices[0].Message.ToolCalls,
		}
		if response.Choices[0].Message.Content != nil {
			assistantMsg["content"] = *response.Choices[0].Message.Content
		}
		msgs = append(msgs, assistantMsg)

		for _, tr := range toolResultMsgs {
			msgs = append(msgs, tr)
		}
		currentReq["messages"] = msgs

		followupBytes, err := json.Marshal(currentReq)
		if err != nil {
			break
		}

		// Send follow-up request
		apiKey := h.resolveAPIKey(routing)
		timeouts := config.GetModelTimeouts(routing.Model)

		followupRespBytes, err := h.executeToolContinuationOpenAIWithRetry(routing.BaseURL, apiKey, path, followupBytes, timeouts, routing.Model)
		if err != nil {
			h.runnerLogger.Printf("[tool_execution] follow-up request failed at round %d: %v", round+1, err)
			break
		}

		// Try text extraction on follow-up too
		followupRespBytes = anthropiccompat.ExtractToolCallsFromText(followupRespBytes)
		currentRespBytes = followupRespBytes
	}

	h.runnerLogger.Printf("[tool_execution] completed with %d input + %d output tokens", totalInputTokens, totalOutputTokens)

	// Convert final response to Anthropic format
	anthropicResp, _, _, tc, _, err := anthropiccompat.OpenAIResponseToAnthropic(currentRespBytes, model)
	if err != nil {
		return currentRespBytes, totalInputTokens, totalOutputTokens, hasToolCall, err
	}
	return anthropicResp, totalInputTokens, totalOutputTokens, hasToolCall || tc, nil
}

// executeToolContinuationOpenAIWithRetry sends a follow-up request to an OpenAI-compat endpoint with retry logic.
func (h *Handler) executeToolContinuationOpenAIWithRetry(baseURL, apiKey, path string, requestBytes []byte, timeouts config.ModelTimeouts, model string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= timeouts.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * timeouts.RetryDelay
			h.runnerLogger.Printf("[tool_execution] retry %d/%d for model=%s, waiting %v", attempt, timeouts.MaxRetries, model, delay)
			time.Sleep(delay)
		}

		resp, err := openaicompat.Proxy(baseURL, apiKey, path, requestBytes)
		if err != nil {
			lastErr = err
			h.runnerLogger.Printf("[tool_execution] attempt %d failed for model=%s: %v", attempt+1, model, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = err
			h.runnerLogger.Printf("[tool_execution] server error %d on attempt %d for model=%s", resp.StatusCode, attempt+1, model)
			continue
		} else if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp.Body)
			h.runnerLogger.Printf("[tool_execution] client error %d for model=%s: %s", resp.StatusCode, model, string(respBody))
			return respBody, err
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		h.runnerLogger.Printf("[tool_execution] successful continuation on attempt %d for model=%s", attempt+1, model)
		return respBody, nil
	}

	return nil, lastErr
}

// isThinkingLoop detects when a model is stuck reasoning about calling tools
// without actually producing tool_calls. Common with smaller/weaker models.
func isThinkingLoop(content string, threshold int) bool {
	if len(content) < threshold {
		return false
	}
	lower := strings.ToLower(content)
	indicators := []string{
		"i need to", "i should", "let me", "the correct approach",
		"tool call", "tool_call", "todowrite", "function_call",
		"the assistant should", "the user", "each todo",
		"i'll call", "i will call", "let's call",
		"the next step", "so the correct", "so i should",
	}
	hits := 0
	for _, ind := range indicators {
		hits += strings.Count(lower, ind)
	}
	return hits >= 3
}

