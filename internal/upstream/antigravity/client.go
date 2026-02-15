package antigravity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// ExchangeRefreshToken exchanges a Google refresh token for an access token
func ExchangeRefreshToken(refreshToken string) (string, error) {
	data := url.Values{}
	data.Set("client_id", "764086051744-9g1o7scj9sc9989p7c1i88t0j8847m8r.apps.googleusercontent.com") // Cloud Code Client ID
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}

	return tr.AccessToken, nil
}

// ProxyToGoogle sends the transformed request to Antigravity's Google API
func ProxyToGoogle(accessToken string, model string, body []byte, stream bool) (*http.Response, error) {
	// Antigravity Cloud Code Endpoint
	// Example: https://daily-cloudcode-pa.sandbox.googleapis.com/v1/projects/antigravity/locations/global/publishers/google/models/claude-3-5-sonnet:streamGenerateContent
	
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}

	// Map generic names to Antigravity specific names if needed
	googleModel := model
	if !strings.Contains(googleModel, "publishers/") {
		// Defaulting to a common path if not full path provided
		googleModel = fmt.Sprintf("publishers/google/models/%s", model)
	}

	apiURL := fmt.Sprintf("https://daily-cloudcode-pa.sandbox.googleapis.com/v1/projects/antigravity/locations/global/%s:%s", googleModel, action)
	
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 5 * time.Minute}
	return client.Do(req)
}
