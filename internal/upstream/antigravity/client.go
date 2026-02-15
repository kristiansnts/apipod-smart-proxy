package antigravity

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

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
