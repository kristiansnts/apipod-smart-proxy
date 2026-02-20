package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
)

// UsageCommitter sends usage data to the Laravel backend asynchronously.
type UsageCommitter struct {
	baseURL    string
	secret     string
	httpClient *http.Client
	logger     *log.Logger
}

// CommitPayload is sent to POST /api/internal/commit-usage.
type CommitPayload struct {
	RequestID   string `json:"request_id"`
	OrgID       uint   `json:"org_id"`
	APIKeyID    uint   `json:"api_key_id"`
	Model       string `json:"model"`
	InputTokens int    `json:"input_tokens"`
	OutputTokens int   `json:"output_tokens"`
	Mode        string `json:"mode"`
}

func NewUsageCommitter(baseURL, secret string, logger *log.Logger) *UsageCommitter {
	return &UsageCommitter{
		baseURL: baseURL,
		secret:  secret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// CommitAsync sends usage data in a goroutine. Non-blocking.
func (uc *UsageCommitter) CommitAsync(orgID, apiKeyID uint, model, mode string, inputTokens, outputTokens int) {
	go func() {
		requestID := ulid.Make().String()

		payload := CommitPayload{
			RequestID:    requestID,
			OrgID:        orgID,
			APIKeyID:     apiKeyID,
			Model:        model,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Mode:         mode,
		}

		body, err := json.Marshal(payload)
		if err != nil {
			uc.logger.Printf("ERROR [commit] marshal: %v", err)
			return
		}

		// Retry up to 3 times
		for attempt := 1; attempt <= 3; attempt++ {
			err = uc.send(body)
			if err == nil {
				return
			}
			uc.logger.Printf("WARN [commit] attempt %d failed: %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		uc.logger.Printf("ERROR [commit] all retries failed for request_id=%s", requestID)
	}()
}

func (uc *UsageCommitter) send(body []byte) error {
	req, err := http.NewRequest("POST", uc.baseURL+"/api/internal/commit-usage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", uc.secret)

	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("commit-usage returned %d", resp.StatusCode)
	}
	return nil
}
