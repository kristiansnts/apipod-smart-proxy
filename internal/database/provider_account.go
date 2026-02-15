package database

import (
	"time"
)

type ProviderAccount struct {
	ID           uint      `db:"id"`
	ProviderID   uint      `db:"provider_id"`
	Email        string    `db:"email"`
	APIKey       string    `db:"api_key"`
	IsActive     bool      `db:"is_active"`
	LastUsedAt   time.Time `db:"last_used_at"`
}

func (db *DB) GetActiveAccountsForProvider(providerID uint) ([]ProviderAccount, error) {
	rows, err := db.conn.Query("SELECT id, provider_id, email, api_key, is_active, COALESCE(last_used_at, '0001-01-01 00:00:00') FROM provider_accounts WHERE provider_id = $1 AND is_active = true", providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []ProviderAccount
	for rows.Next() {
		var acc ProviderAccount
		if err := rows.Scan(&acc.ID, &acc.ProviderID, &acc.Email, &acc.APIKey, &acc.IsActive, &acc.LastUsedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}
