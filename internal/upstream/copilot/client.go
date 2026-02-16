package copilot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ProxyToCopilot sends the request to cliproxy upstream (GHCP provider)
// Returns the response and the upstream URL for logging purposes
func ProxyToCopilot(baseURL string, ghpToken string, model string, body []byte, stream bool) (*http.Response, string, error) {
	// Cliproxy API Endpoint from database
	apiURL := strings.TrimRight(baseURL, "/") + "/v1/messages"

	// Replace the model name in the request body with the routed model
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return nil, apiURL, err
	}
	bodyMap["model"] = model
	modifiedBody, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, apiURL, err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(modifiedBody))
	if err != nil {
		return nil, apiURL, err
	}

	// Headers for cliproxy upstream
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ghpToken)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	return resp, apiURL, err
}
