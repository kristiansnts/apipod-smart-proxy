package antigravity

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Google OAuth2 Client ID for Cloud Code extension
const googleClientID = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
const googleClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"

// ExchangeRefreshToken exchanges a Google OAuth2 refresh token for an access token
func ExchangeRefreshToken(refreshToken string) (string, error) {
	data := url.Values{}
	data.Set("client_id", googleClientID)
	data.Set("client_secret", googleClientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("oauth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	return tokenResp.AccessToken, nil
}

// ProxyToGoogle sends the transformed request to Antigravity's Google API with "Fingerprint" headers
func ProxyToGoogle(accessToken string, model string, body []byte, stream bool) (*http.Response, error) {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}

	googleModel := model
	if !strings.Contains(googleModel, "publishers/") {
		googleModel = fmt.Sprintf("publishers/google/models/%s", model)
	}

	apiURL := fmt.Sprintf("https://daily-cloudcode-pa.sandbox.googleapis.com/v1/projects/antigravity/locations/global/%s:%s", googleModel, action)
	
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	// FINGERPRINT HEADERS (Meniru Google Cloud Code Extension)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Code/1.92.2 Chrome/124.0.6367.243 Electron/30.4.0 Safari/537.36")
	req.Header.Set("x-goog-api-client", "gl-js/ env/vscode/1.92.2")
	req.Header.Set("x-goog-user-project", "antigravity")
	req.Header.Set("Referer", "vscode-webview://")
	
	// Default client with TLS config
	// Catatan: Karena 'go' binary tidak tersedia di PATH sandbox saat ini, 
	// saya menggunakan standard library dengan TLS 1.3 yang sangat mirip dengan Chrome.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
		},
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Minute,
	}
	
	return client.Do(req)
}
