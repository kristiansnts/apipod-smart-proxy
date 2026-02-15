package copilot

import (
	"encoding/json"
	"fmt"
)

// Simple transform for Copilot (usually OpenAI compatible but with GHP token)
func TransformToCopilot(body []byte) ([]byte, error) {
	// For now, Copilot often accepts standard OpenAI format if routed correctly.
	// We might need to adjust the model name to what GitHub expects.
	return body, nil
}
