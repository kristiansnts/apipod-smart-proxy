package database

type ProviderAccount struct {
	ID         uint   `db:"id"`
	ProviderID uint   `db:"provider_id"`
	Email      string `db:"email"`
	APIKey     string `db:"api_key"`
}

func (db *DB) GetAccountsForProvider(providerID uint) ([]ProviderAccount, error) {
	rows, err := db.conn.Query("SELECT id, provider_id, email, api_key FROM provider_accounts WHERE provider_id = $1", providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []ProviderAccount
	for rows.Next() {
		var acc ProviderAccount
		if err := rows.Scan(&acc.ID, &acc.ProviderID, &acc.Email, &acc.APIKey); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}
