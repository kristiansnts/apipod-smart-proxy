package database

import "fmt"

// UsageContext holds metadata about a request for usage logging
type UsageContext struct {
	QuotaItemID      int64
	UserID           string
	RequestedModel   string
	RoutedModel      string
	UpstreamProvider string
}

// LogUsage inserts a usage log entry for a completed request
func (db *DB) LogUsage(ctx UsageContext, inputTokens, outputTokens int) error {
	totalTokens := inputTokens + outputTokens
	_, err := db.conn.Exec(
		`INSERT INTO usage_logs (quota_item_id, user_id, requested_model, routed_model, upstream_provider, token_count, input_tokens, output_tokens)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ctx.QuotaItemID, ctx.UserID, ctx.RequestedModel, ctx.RoutedModel, ctx.UpstreamProvider,
		totalTokens, inputTokens, outputTokens,
	)
	if err != nil {
		return fmt.Errorf("failed to log usage: %w", err)
	}
	return nil
}
