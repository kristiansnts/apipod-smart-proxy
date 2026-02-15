package database

import "fmt"

// LogUsage inserts a usage log entry for a completed request
func (db *DB) LogUsage(quotaItemID int64, inputTokens, outputTokens int) error {
	totalTokens := inputTokens + outputTokens
	_, err := db.conn.Exec(
		`INSERT INTO usage_logs (quota_item_id, token_count, input_tokens, output_tokens) VALUES ($1, $2, $3, $4)`,
		quotaItemID, totalTokens, inputTokens, outputTokens,
	)
	if err != nil {
		return fmt.Errorf("failed to log usage: %w", err)
	}
	return nil
}
