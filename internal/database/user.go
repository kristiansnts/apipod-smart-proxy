package database

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
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

// newULID generates a new ULID using a cryptographically secure source
func newULID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate ULID: %w", err)
	}
	return id.String(), nil
}

// CreateUser inserts a new user into the database with a ULID primary key
func (db *DB) CreateUser(username, apiToken string, subID int64, expiresAt *time.Time) (*User, error) {
	userID, err := newULID()
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO users (user_id, username, apitoken, sub_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING user_id, username, apitoken, sub_id, active, created_at, expires_at
	`

	var user User
	var expiresAtSQL sql.NullTime

	err = db.conn.QueryRow(query, userID, username, apiToken, subID, expiresAt).Scan(
		&user.ID,
		&user.Username,
		&user.APIToken,
		&user.SubID,
		&user.Active,
		&user.CreatedAt,
		&expiresAtSQL,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	if expiresAtSQL.Valid {
		user.ExpiresAt = &expiresAtSQL.Time
	}

	return &user, nil
}

// GetUserByAPIToken retrieves a user (with subscription name) by their API token
func (db *DB) GetUserByAPIToken(apiToken string) (*User, error) {
	query := `
		SELECT u.user_id, u.username, u.apitoken, u.sub_id,
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
