package database

import "fmt"

// QuotaItem represents a weighted model entry for a subscription plan
type QuotaItem struct {
	QuotaID          int64
	SubID            int64
	LLMModelID       int64
	ModelName        string
	PercentageWeight int
	BaseURL          string
	APIKey           string
	ProviderType     string
	ProviderID       int64
}

// GetQuotaItemsBySubID loads all quota items (with model and provider info) for a subscription
func (db *DB) GetQuotaItemsBySubID(subID int64) ([]QuotaItem, error) {
	query := `
		SELECT qi.quota_id, qi.sub_id, qi.llm_model_id,
		       m.model_name, qi.percentage_weight,
		       p.base_url, COALESCE(p.api_key, ''), p.provider_type, p.id
		FROM quota_items qi
		JOIN llm_models m ON m.llm_model_id = qi.llm_model_id
		JOIN providers p ON p.id = m.provider_id
		WHERE qi.sub_id = $1
	`

	rows, err := db.conn.Query(query, subID)
	if err != nil {
		return nil, fmt.Errorf("failed to query quota items: %w", err)
	}
	defer rows.Close()

	var items []QuotaItem
	for rows.Next() {
		var qi QuotaItem
		if err := rows.Scan(
			&qi.QuotaID, &qi.SubID, &qi.LLMModelID,
			&qi.ModelName, &qi.PercentageWeight,
			&qi.BaseURL, &qi.APIKey, &qi.ProviderType, &qi.ProviderID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan quota item: %w", err)
		}
		items = append(items, qi)
	}
	return items, rows.Err()
}
