package database

import (
	"database/sql"
	"fmt"
	"time"
)

// User represents a user with an API token and subscription plan
type User struct {
	ID        string
	Username  string
	APIToken  string
	SubID     int64
	SubName   string
	Active    bool
	CreatedAt time.Time
	ExpiresAt *time.Time
}

// GetUserByAPIToken retrieves a user (with subscription name) by their API token
func (db *DB) GetUserByAPIToken(apiToken string) (*User, error) {
	query := `
		SELECT u.id::text, u.name, u.apitoken, u.sub_id,
		       COALESCE(s.sub_name, ''), u.active, u.created_at, u.expires_at
		FROM users u
		LEFT JOIN subscriptions s ON s.sub_id = u.sub_id
		WHERE u.apitoken = $1
	`

	var user User
	var expiresAtSQL sql.NullTime

	err := db.conn.QueryRow(query, apiToken).Scan(
		&user.ID,
		&user.Username,
		&user.APIToken,
		&user.SubID,
		&user.SubName,
		&user.Active,
		&user.CreatedAt,
		&expiresAtSQL,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if expiresAtSQL.Valid {
		user.ExpiresAt = &expiresAtSQL.Time
	}

	return &user, nil
}

// IsValidAPIToken checks if an API token is valid (exists, active, not expired)
func (db *DB) IsValidAPIToken(apiToken string) (bool, *User, error) {
	user, err := db.GetUserByAPIToken(apiToken)
	if err != nil {
		return false, nil, err
	}
	if user == nil {
		return false, nil, nil
	}
	if !user.Active {
		return false, user, nil
	}
	if user.ExpiresAt != nil && user.ExpiresAt.Before(time.Now()) {
		return false, user, nil
	}
	return true, user, nil
}
