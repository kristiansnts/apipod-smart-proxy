package database

import (
	"database/sql"
	"fmt"
)

// Subscription represents a subscription plan
type Subscription struct {
	SubID   int64
	SubName string
	Price   string
}

// GetSubscriptionByName retrieves a subscription by its plan name
func (db *DB) GetSubscriptionByName(name string) (*Subscription, error) {
	query := `SELECT sub_id, sub_name, COALESCE(price, '') FROM subscriptions WHERE sub_name = $1`

	var s Subscription
	err := db.conn.QueryRow(query, name).Scan(&s.SubID, &s.SubName, &s.Price)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}
	return &s, nil
}
