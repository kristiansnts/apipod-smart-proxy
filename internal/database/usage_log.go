package database

import "fmt"

// LogUsage inserts a usage log entry for a completed request
func (db *DB) LogUsage(quotaItemID int64, tokenCount int) error {
	_, err := db.conn.Exec(
		`INSERT INTO usage_logs (quota_item_id, token_count) VALUES ($1, $2)`,
		quotaItemID, tokenCount,
	)
	if err != nil {
		return fmt.Errorf("failed to log usage: %w", err)
	}
	return nil
}
