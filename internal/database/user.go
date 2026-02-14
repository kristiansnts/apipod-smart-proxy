package database

import (
	"database/sql"
	"fmt"
	"time"
)

// User represents a user with an API key
type User struct {
	ID        int64
	APIKey    string
	Name      string
	Tier      string
	Active    bool
	CreatedAt time.Time
	ExpiresAt *time.Time
}

// CreateUser inserts a new user into the database
func (db *DB) CreateUser(name, apiKey, tier string, expiresAt *time.Time) (*User, error) {
	query := `
		INSERT INTO users (api_key, name, tier, expires_at)
		VALUES (?, ?, ?, ?)
		RETURNING id, api_key, name, tier, active, created_at, expires_at
	`

	var user User
	var active int
	var expiresAtSQL sql.NullTime

	err := db.conn.QueryRow(query, apiKey, name, tier, expiresAt).Scan(
		&user.ID,
		&user.APIKey,
		&user.Name,
		&user.Tier,
		&active,
		&user.CreatedAt,
		&expiresAtSQL,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user.Active = active == 1
	if expiresAtSQL.Valid {
		user.ExpiresAt = &expiresAtSQL.Time
	}

	return &user, nil
}

// GetUserByAPIKey retrieves a user by their API key
func (db *DB) GetUserByAPIKey(apiKey string) (*User, error) {
	query := `
		SELECT id, api_key, name, tier, active, created_at, expires_at
		FROM users
		WHERE api_key = ?
	`

	var user User
	var active int
	var expiresAtSQL sql.NullTime

	err := db.conn.QueryRow(query, apiKey).Scan(
		&user.ID,
		&user.APIKey,
		&user.Name,
		&user.Tier,
		&active,
		&user.CreatedAt,
		&expiresAtSQL,
	)

	if err == sql.ErrNoRows {
		return nil, nil // User not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	user.Active = active == 1
	if expiresAtSQL.Valid {
		user.ExpiresAt = &expiresAtSQL.Time
	}

	return &user, nil
}

// IsValidAPIKey checks if an API key is valid (exists, active, not expired)
func (db *DB) IsValidAPIKey(apiKey string) (bool, *User, error) {
	user, err := db.GetUserByAPIKey(apiKey)
	if err != nil {
		return false, nil, err
	}
	if user == nil {
		return false, nil, nil // Key doesn't exist
	}

	// Check if user is active
	if !user.Active {
		return false, user, nil
	}

	// Check if key has expired
	if user.ExpiresAt != nil && user.ExpiresAt.Before(time.Now()) {
		return false, user, nil
	}

	return true, user, nil
}
