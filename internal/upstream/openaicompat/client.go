package openaicompat

import (
	"bytes"
	"net/http"
	"strings"
	"time"
)

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     120 * time.Second,
}

// Proxy sends the request body as-is to an OpenAI-compatible endpoint.
// The path is appended to the base URL, allowing different providers to use different endpoints.
// e.g. Groq: baseURL="https://api.groq.com", path="/openai/v1/responses"
//      OpenAI: baseURL="https://api.openai.com", path="/v1/chat/completions"
func Proxy(baseURL string, apiKey string, path string, body []byte) (*http.Response, error) {
	apiURL := strings.TrimRight(baseURL, "/") + path

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}
	return client.Do(req)
}
