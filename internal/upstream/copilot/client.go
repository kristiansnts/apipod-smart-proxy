package copilot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProxyToCopilot sends the request to GitHub Copilot API
func ProxyToCopilot(ghpToken string, model string, body []byte, stream bool) (*http.Response, error) {
	// GitHub Copilot API Endpoint
	apiURL := "https://api.githubcopilot.com/chat/completions"

	req, err := http.NewRequest("POST", apiURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Important headers for Copilot
	req.Header.Set("Authorization", "Bearer "+ghpToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.90.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.15.0")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.15.0")

	// Replace body
	req.Body = io.NopCloser(strings.NewReader(string(body)))

	client := &http.Client{Timeout: 5 * time.Minute}
	return client.Do(req)
}
