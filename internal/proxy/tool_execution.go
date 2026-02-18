package proxy

import (
	"encoding/json"
	"io"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/tools"
	"github.com/rpay/apipod-smart-proxy/internal/upstream/anthropiccompat"
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
	var response AnthropicResponse
	if err := json.Unmarshal(respBytes, &response); err != nil {
		return respBytes, 0, 0, err
	}

	// Check if response contains tool_use
	hasTools := false
	var toolCalls []tools.ToolCall
	
	for _, content := range response.Content {
		if content.Type == "tool_use" {
			hasTools = true
			toolCalls = append(toolCalls, tools.ToolCall{
				ID:    content.ID,
				Name:  content.Name,
				Input: content.Input,
			})
		}
	}

	if !hasTools {
		return respBytes, response.Usage.InputTokens, response.Usage.OutputTokens, nil
	}

	h.runnerLogger.Printf("[tool_execution] executing %d tools", len(toolCalls))

	// Execute tools with progress logging
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

	// Parse original request to continue conversation
	var originalReq AnthropicRequest
	if err := json.Unmarshal(originalRequest, &originalReq); err != nil {
		return respBytes, response.Usage.InputTokens, response.Usage.OutputTokens, err
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

	originalReq.Messages = append(originalReq.Messages, map[string]interface{}{
		"role":    "assistant",
		"content": assistantContent,
	})

	// Add tool results as user message
	originalReq.Messages = append(originalReq.Messages, map[string]interface{}{
		"role":    "user", 
		"content": toolResults,
	})

	// Make follow-up request
	followupBytes, err := json.Marshal(originalReq)
	if err != nil {
		return respBytes, response.Usage.InputTokens, response.Usage.OutputTokens, err
	}

	// Get API key for follow-up request
	apiKey := h.resolveAPIKey(routing)
	
	// Get model-specific timeouts and retry configuration
	timeouts := config.GetModelTimeouts(routing.Model)
	
	followupBytes, err = h.executeToolContinuationWithRetry(routing.BaseURL, apiKey, followupBytes, timeouts, routing.Model)
	if err != nil {
		h.runnerLogger.Printf("[tool_execution] follow-up request failed after retries: %v", err)
		return respBytes, response.Usage.InputTokens, response.Usage.OutputTokens, err
	}

	// Parse follow-up response for token counting
	var followupResponse AnthropicResponse
	totalInputTokens := response.Usage.InputTokens
	totalOutputTokens := response.Usage.OutputTokens
	
	if json.Unmarshal(followupBytes, &followupResponse) == nil {
		totalInputTokens += followupResponse.Usage.InputTokens
		totalOutputTokens += followupResponse.Usage.OutputTokens
	}

	h.runnerLogger.Printf("[tool_execution] completed with %d input + %d output tokens", 
		totalInputTokens, totalOutputTokens)

	return followupBytes, totalInputTokens, totalOutputTokens, nil
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


