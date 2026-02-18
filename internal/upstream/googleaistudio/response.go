package googleaistudio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ExtractTokens extracts token usage from a non-streaming Gemini response.
func ExtractTokens(body []byte) (int, int) {
	var resp geminiResponse
	if json.Unmarshal(body, &resp) != nil || resp.UsageMetadata == nil {
		return 0, 0
	}
	return resp.UsageMetadata.PromptTokenCount, resp.UsageMetadata.CandidatesTokenCount
}

// StreamTransformToOpenAI converts a Gemini SSE stream to OpenAI SSE format.
// Returns input tokens, output tokens, and whether a tool call was detected.
func StreamTransformToOpenAI(r io.Reader, w io.Writer, model string) (int, int, bool) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, len(buf))

	inputTokens, outputTokens := 0, 0
	hasToolCall := false
	chunkIndex := 0

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(data) == "[DONE]" {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(interface{ Flush() }); ok {
				f.Flush()
			}
			break
		}

		var gemResp geminiResponse
		if err := json.Unmarshal([]byte(data), &gemResp); err != nil {
			continue
		}

		// Extract tokens
		if gemResp.UsageMetadata != nil {
			inputTokens = gemResp.UsageMetadata.PromptTokenCount
			outputTokens = gemResp.UsageMetadata.CandidatesTokenCount
		}

		// Convert each candidate chunk to OpenAI stream chunk
		if len(gemResp.Candidates) > 0 {
			cand := gemResp.Candidates[0]

			for _, part := range cand.Content.Parts {
				chunk := map[string]interface{}{
					"id":      fmt.Sprintf("chatcmpl-gemini-%d", chunkIndex),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
				}

				if part.FunctionCall != nil {
					hasToolCall = true
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					toolCallData := map[string]interface{}{
						"index": 0,
						"id":    "call_" + part.FunctionCall.Name,
						"type":  "function",
						"function": map[string]interface{}{
							"name":      part.FunctionCall.Name,
							"arguments": string(argsBytes),
						},
					}
					// Include thoughtSignature if present
					if part.ThoughtSignature != "" {
						toolCallData["extra_content"] = map[string]interface{}{
							"google": map[string]interface{}{
								"thought_signature": part.ThoughtSignature,
							},
						}
					}
					chunk["choices"] = []map[string]interface{}{
						{
							"index":         0,
							"delta":         map[string]interface{}{"tool_calls": []map[string]interface{}{toolCallData}},
							"finish_reason": nil,
						},
					}
				} else {
					chunk["choices"] = []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"content": part.Text,
							},
							"finish_reason": nil,
						},
					}
				}

				chunkBytes, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				if f, ok := w.(interface{ Flush() }); ok {
					f.Flush()
				}
				chunkIndex++
			}

			// Emit finish reason on last chunk
			if cand.FinishReason != "" && cand.FinishReason != "FINISH_REASON_UNSPECIFIED" {
				finishReason := "stop"
				switch cand.FinishReason {
				case "MAX_TOKENS":
					finishReason = "length"
				case "STOP":
					finishReason = "stop"
				}
				if hasToolCall {
					finishReason = "tool_calls"
				}

				finishChunk := map[string]interface{}{
					"id":      fmt.Sprintf("chatcmpl-gemini-%d", chunkIndex),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"delta":         map[string]interface{}{},
							"finish_reason": finishReason,
						},
					},
				}

				// Include usage in the final chunk
				if gemResp.UsageMetadata != nil {
					finishChunk["usage"] = map[string]interface{}{
						"prompt_tokens":     inputTokens,
						"completion_tokens": outputTokens,
						"total_tokens":      inputTokens + outputTokens,
					}
				}

				chunkBytes, _ := json.Marshal(finishChunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				if f, ok := w.(interface{ Flush() }); ok {
					f.Flush()
				}
				chunkIndex++
			}
		}
	}

	// Send [DONE] if not already sent
	if chunkIndex > 0 {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(interface{ Flush() }); ok {
			f.Flush()
		}
	}

	return inputTokens, outputTokens, hasToolCall
}
