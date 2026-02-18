package database

import (
	"database/sql"
	"fmt"
)

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
	RPM              *int
	TPM              *int
	RPD              *int
}

// GetQuotaItemsBySubID loads all quota items (with model and provider info) for a subscription
func (db *DB) GetQuotaItemsBySubID(subID int64) ([]QuotaItem, error) {
	query := `
		SELECT qi.quota_id, qi.sub_id, qi.llm_model_id,
		       m.model_name, qi.percentage_weight,
		       p.base_url, COALESCE(p.api_key, ''), p.provider_type, p.id,
		       m.rpm, m.tpm, m.rpd
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
		var rpm, tpm, rpd sql.NullInt64
		if err := rows.Scan(
			&qi.QuotaID, &qi.SubID, &qi.LLMModelID,
			&qi.ModelName, &qi.PercentageWeight,
			&qi.BaseURL, &qi.APIKey, &qi.ProviderType, &qi.ProviderID,
			&rpm, &tpm, &rpd,
		); err != nil {
			return nil, fmt.Errorf("failed to scan quota item: %w", err)
		}
		if rpm.Valid {
			v := int(rpm.Int64)
			qi.RPM = &v
		}
		if tpm.Valid {
			v := int(tpm.Int64)
			qi.TPM = &v
		}
		if rpd.Valid {
			v := int(rpd.Int64)
			qi.RPD = &v
		}
		items = append(items, qi)
	}
	return items, rows.Err()
}
