package database

import "fmt"

// Seed inserts the default subscriptions, LLM models, and quota items.
// All inserts are idempotent (ON CONFLICT DO NOTHING).
func (db *DB) Seed() error {
	// ── Subscriptions ────────────────────────────────────────────────────────
	subs := []struct{ name, price string }{
		{"cursor-pro-auto", "50K/month"},
		{"cursor-pro-sonnet", "75K/month"},
		{"cursor-pro-opus", "pending"},
	}
	for _, s := range subs {
		if _, err := db.conn.Exec(
			`INSERT INTO subscriptions (sub_name, price) VALUES ($1, $2) ON CONFLICT (sub_name) DO NOTHING`,
			s.name, s.price,
		); err != nil {
			return fmt.Errorf("seed subscription %q: %w", s.name, err)
		}
	}

	// ── LLM Models ───────────────────────────────────────────────────────────
	// Each row is (model_name, upstream). Duplicates are skipped.
	models := []struct{ name, upstream string }{
		{"gemini-3-flash", "antigravity"},
		{"gpt-5-mini", "ghcp"},
		{"claude-sonnet-4-5-thinking", "antigravity"},
		{"claude-sonnet-4.5", "ghcp"},
		{"claude-opus-4-5-thinking", "antigravity"},
		{"claude-opus-4-6-thinking", "antigravity"},
		{"claude-opus-4-6-thinking", "ghcp"},
	}
	for _, m := range models {
		if _, err := db.conn.Exec(
			`INSERT INTO llm_models (model_name, upstream) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			m.name, m.upstream,
		); err != nil {
			return fmt.Errorf("seed llm_model %q/%q: %w", m.name, m.upstream, err)
		}
	}

	// ── Helper: resolve IDs ───────────────────────────────────────────────────
	subID := func(name string) int64 {
		var id int64
		db.conn.QueryRow(`SELECT sub_id FROM subscriptions WHERE sub_name = $1`, name).Scan(&id)
		return id
	}
	modelID := func(name, upstream string) int64 {
		var id int64
		db.conn.QueryRow(
			`SELECT llm_model_id FROM llm_models WHERE model_name = $1 AND upstream = $2`,
			name, upstream,
		).Scan(&id)
		return id
	}

	// ── Quota Items ───────────────────────────────────────────────────────────
	type quotaRow struct {
		subName  string
		model    string
		upstream string
		weight   int
	}

	quotas := []quotaRow{
		// cursor-pro-auto (total weight 100)
		{"cursor-pro-auto", "gemini-3-flash", "antigravity", 50},
		{"cursor-pro-auto", "gpt-5-mini", "ghcp", 50},

		// cursor-pro-sonnet (total weight 100)
		{"cursor-pro-sonnet", "claude-sonnet-4-5-thinking", "antigravity", 20},
		{"cursor-pro-sonnet", "claude-sonnet-4.5", "ghcp", 10},
		{"cursor-pro-sonnet", "gemini-3-flash", "antigravity", 40},
		{"cursor-pro-sonnet", "gpt-5-mini", "ghcp", 30},

		// cursor-pro-opus (total weight 130, normalized at runtime)
		{"cursor-pro-opus", "claude-opus-4-5-thinking", "antigravity", 20},
		{"cursor-pro-opus", "claude-opus-4-6-thinking", "antigravity", 10},
		{"cursor-pro-opus", "claude-opus-4-6-thinking", "ghcp", 10},
		{"cursor-pro-opus", "claude-sonnet-4.5", "ghcp", 30},
		{"cursor-pro-opus", "gemini-3-flash", "antigravity", 30},
		{"cursor-pro-opus", "gpt-5-mini", "ghcp", 30},
	}

	for _, q := range quotas {
		sid := subID(q.subName)
		mid := modelID(q.model, q.upstream)
		if sid == 0 || mid == 0 {
			return fmt.Errorf("seed quota: could not resolve sub=%q model=%q/%q", q.subName, q.model, q.upstream)
		}
		if _, err := db.conn.Exec(
			`INSERT INTO quota_items (sub_id, llm_model_id, percentage_weight)
			 SELECT $1, $2, $3
			 WHERE NOT EXISTS (
			     SELECT 1 FROM quota_items WHERE sub_id = $1 AND llm_model_id = $2
			 )`,
			sid, mid, q.weight,
		); err != nil {
			return fmt.Errorf("seed quota_item sub=%q model=%q: %w", q.subName, q.model, err)
		}
	}

	return nil
}
