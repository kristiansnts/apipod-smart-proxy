package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// DB wraps the PostgreSQL database connection
type DB struct {
	conn *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS subscriptions (
    sub_id   SERIAL PRIMARY KEY,
    sub_name VARCHAR(100) UNIQUE NOT NULL,
    price    TEXT
);

CREATE TABLE IF NOT EXISTS llm_models (
    llm_model_id SERIAL PRIMARY KEY,
    model_name   VARCHAR(200) NOT NULL,
    upstream     VARCHAR(50)  NOT NULL,
    UNIQUE (model_name, upstream) -- ADDED UNIQUE CONSTRAINT HERE
);

CREATE TABLE IF NOT EXISTS quota_items (
    quota_id          SERIAL PRIMARY KEY,
    sub_id            INTEGER NOT NULL REFERENCES subscriptions(sub_id),
    llm_model_id      INTEGER NOT NULL REFERENCES llm_models(llm_model_id),
    percentage_weight INTEGER NOT NULL,
    UNIQUE (sub_id, llm_model_id) -- ADDED UNIQUE CONSTRAINT HERE for quota_items as well
);

CREATE TABLE IF NOT EXISTS users (
    user_id    VARCHAR(26)  PRIMARY KEY,
    username   VARCHAR(200),
    apitoken   VARCHAR(200) UNIQUE NOT NULL,
    sub_id     INTEGER REFERENCES subscriptions(sub_id),
    active     BOOLEAN   DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW(),
    expires_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS usage_logs (
    usage_id      SERIAL PRIMARY KEY,
    quota_item_id INTEGER NOT NULL REFERENCES quota_items(quota_id),
    token_count   INTEGER DEFAULT 0,
    input_tokens  INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    timestamp     TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_apitoken    ON users(apitoken);
CREATE INDEX IF NOT EXISTS idx_user_active ON users(active);
CREATE INDEX IF NOT EXISTS idx_quota_sub   ON quota_items(sub_id);
CREATE INDEX IF NOT EXISTS idx_usage_quota ON usage_logs(quota_item_id);

ALTER TABLE llm_models ADD COLUMN IF NOT EXISTS rpm INTEGER;
ALTER TABLE llm_models ADD COLUMN IF NOT EXISTS tpm INTEGER;
ALTER TABLE llm_models ADD COLUMN IF NOT EXISTS rpd INTEGER;
`

// New creates a new PostgreSQL database connection and initializes the schema
func New(dsn string) (*DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}

	if err := db.initSchema(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) initSchema() error {
	if _, err := db.conn.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}
