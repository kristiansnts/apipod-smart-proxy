package googleaistudio

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     120 * time.Second,
}

// Proxy sends a Gemini-format request to Google AI Studio.
// For streaming, it uses the streamGenerateContent endpoint with alt=sse.
func Proxy(baseURL string, apiKey string, model string, body []byte, stream bool) (*http.Response, error) {
	base := strings.TrimRight(baseURL, "/")
	var apiURL string
	if stream {
		apiURL = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", base, model)
	} else {
		apiURL = fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, model)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-goog-api-key", apiKey)

	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}
	return client.Do(req)
}
