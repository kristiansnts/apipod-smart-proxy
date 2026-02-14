# Plan: PostgreSQL Migration + Subscription Plans

## Objective
Replace SQLite with PostgreSQL and implement a database-driven subscription plan system
with three tiers (Cursor-Pro-Auto, Cursor-Pro-Sonnet, Cursor-Pro-Opus), routing based on
per-plan weighted model distribution, and usage logging.

## Token Estimate
~40K tokens — moderate scope, ~10 files modified/created

## Files Affected

### Modified
- `go.mod` — add `github.com/lib/pq`, remove `modernc.org/sqlite` and all indirect SQLite deps
- `internal/config/config.go` — replace `DatabasePath` with `DatabaseURL`
- `internal/database/database.go` — replace SQLite driver with PostgreSQL, update schema
- `internal/database/user.go` — new schema: `user_id`, `username`, `apitoken`, `sub_id` FK
- `internal/proxy/router.go` — DB-driven routing using quota_items per subscription
- `internal/proxy/handler.go` — pass `sub_id` to router, log usage after request
- `internal/admin/handler.go` — accept `sub_name` (plan name) instead of `tier`
- `cmd/server/main.go` — pass DB to router, update logging
- `.env.example` — add `DATABASE_URL`, remove `DATABASE_PATH`

### Created
- `internal/database/subscription.go` — subscription queries
- `internal/database/quota_item.go` — quota item + LLM model queries
- `internal/database/usage_log.go` — usage log insert query
- `internal/database/seed.go` — seed subscriptions, llm_models, quota_items on startup

## Database Schema (PostgreSQL)

```sql
CREATE TABLE subscriptions (
    sub_id   SERIAL PRIMARY KEY,
    sub_name VARCHAR(100) UNIQUE NOT NULL,
    price    TEXT
);

CREATE TABLE llm_models (
    llm_model_id SERIAL PRIMARY KEY,
    model_name   VARCHAR(200) NOT NULL,
    upstream     VARCHAR(50)  NOT NULL  -- 'antigravity' or 'ghcp'
);

CREATE TABLE quota_items (
    quota_id           SERIAL PRIMARY KEY,
    sub_id             INTEGER NOT NULL REFERENCES subscriptions(sub_id),
    llm_model_id       INTEGER NOT NULL REFERENCES llm_models(llm_model_id),
    percentage_weight  INTEGER NOT NULL
);

CREATE TABLE users (
    user_id    SERIAL PRIMARY KEY,
    username   VARCHAR(200),
    apitoken   VARCHAR(200) UNIQUE NOT NULL,
    sub_id     INTEGER REFERENCES subscriptions(sub_id),
    active     BOOLEAN   DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW(),
    expires_at TIMESTAMP
);

CREATE TABLE usage_logs (
    usage_id       SERIAL PRIMARY KEY,
    quota_item_id  INTEGER NOT NULL REFERENCES quota_items(quota_id),
    token_count    INTEGER DEFAULT 0,
    timestamp      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_apitoken   ON users(apitoken);
CREATE INDEX idx_user_active ON users(active);
CREATE INDEX idx_quota_sub  ON quota_items(sub_id);
CREATE INDEX idx_usage_quota ON usage_logs(quota_item_id);
```

## Seed Data

### Subscriptions
| sub_name           | price        |
|--------------------|--------------|
| cursor-pro-auto    | 50K/month    |
| cursor-pro-sonnet  | 75K/month    |
| cursor-pro-opus    | pending      |

### LLM Models
| model_name                   | upstream     |
|------------------------------|--------------|
| gemini-3-flash               | antigravity  |
| gpt-5-mini                   | ghcp         |
| claude-sonnet-4-5-thinking   | antigravity  |
| claude-sonnet-4.5            | ghcp         |
| claude-opus-4-5-thinking     | antigravity  |
| claude-opus-4-6-thinking     | antigravity  |
| claude-opus-4-6-thinking     | ghcp         |  ← separate row, different upstream

### Quota Items

**cursor-pro-auto** (total weight 100):
| model                 | upstream    | weight |
|-----------------------|-------------|--------|
| gemini-3-flash        | antigravity | 50     |
| gpt-5-mini            | ghcp        | 50     |

**cursor-pro-sonnet** (total weight 100):
| model                        | upstream    | weight |
|------------------------------|-------------|--------|
| claude-sonnet-4-5-thinking   | antigravity | 20     |
| claude-sonnet-4.5            | ghcp        | 10     |
| gemini-3-flash               | antigravity | 40     |
| gpt-5-mini                   | ghcp        | 30     |

**cursor-pro-opus** (total weight 130, normalized at runtime):
| model                      | upstream    | weight |
|----------------------------|-------------|--------|
| claude-opus-4-5-thinking   | antigravity | 20     |
| claude-opus-4-6-thinking   | antigravity | 10     |
| claude-opus-4-6-thinking   | ghcp        | 10     |  ← "Copilot" slot
| claude-sonnet-4.5          | ghcp        | 30     |
| gemini-3-flash             | antigravity | 30     |
| gpt-5-mini                 | ghcp        | 30     |

> NOTE: Weights are relative (not strict %). Weighted random selection normalizes them.
> For cursor-pro-opus the "Copilot" (10%) slot is assumed to be claude-opus-4-6-thinking
> via GHCP. Please confirm if a different model should be used.

## Architecture Changes

### Router (proxy/router.go)
- `Router` gains a `db *database.DB` field
- New signature: `RouteModel(subID int64) (RoutingResult, int64, error)`
  - Returns `RoutingResult` + `quota_item_id` (for usage logging)
- Loads quota items from DB for the user's subscription
- Weighted random selection:
  ```go
  totalWeight := sum(item.weight for item in items)
  roll := rand.Intn(totalWeight)
  // iterate items, subtract weights until <= 0, pick that item
  ```

### Handler (proxy/handler.go)
- Gets `user.SubID` from context (set by auth middleware)
- Calls `router.RouteModel(user.SubID)` → gets `quotaItemID`
- After upstream response, calls `db.LogUsage(quotaItemID, tokenCount)`
- Non-streaming: extracts token_count from JSON response `usage.input_tokens + usage.output_tokens`
- Streaming: logs with token_count = 0 (to be improved later)
- Handler needs `db *database.DB` added to its struct

### User Model (database/user.go)
```go
type User struct {
    ID        int64
    Username  string
    APIToken  string
    SubID     int64
    SubName   string  // loaded via JOIN for convenience
    Active    bool
    CreatedAt time.Time
    ExpiresAt *time.Time
}
```

### Admin Handler (admin/handler.go)
Request body changes:
```json
{
  "name": "alice",
  "sub_name": "cursor-pro-sonnet",  // was "tier"
  "expires_in": 30
}
```
- Looks up `sub_id` from `sub_name` before creating user

### Config (config/config.go)
- Remove `DatabasePath`, add `DatabaseURL`
- Default: `postgres://local:local@127.0.0.1/apipod?sslmode=disable`

## Implementation Steps

1. **Add PostgreSQL driver** — `go get github.com/lib/pq`, update `go.mod`
2. **Update config** — replace `DatabasePath` with `DatabaseURL` in `config.go`
3. **Rewrite database.go** — PostgreSQL connection, new schema, `$1/$2/...` placeholders
4. **Create seed.go** — idempotent seed using `INSERT ... ON CONFLICT DO NOTHING`
5. **Update user.go** — new column names, JOIN with subscriptions on read
6. **Create subscription.go** — `GetSubscriptionByName(name) (*Subscription, error)`
7. **Create quota_item.go** — `GetQuotaItemsBySubID(subID) ([]QuotaItem, error)`
8. **Create usage_log.go** — `LogUsage(quotaItemID int64, tokenCount int) error`
9. **Update router.go** — DB-driven weighted routing, return quota_item_id
10. **Update handler.go** — add `db` field, use new router signature, log usage
11. **Update admin/handler.go** — accept `sub_name`, resolve to `sub_id`
12. **Update main.go** — pass DB to router and handler, update startup logs
13. **Update .env.example** — add `DATABASE_URL`

## PostgreSQL Connection String
```
DATABASE_URL=postgresql://local:local@127.0.0.1/apipod?sslmode=disable
```

## Risks & Considerations
- PostgreSQL uses `$1, $2, ...` placeholders (not `?` like SQLite)
- `RETURNING` clause works the same in PostgreSQL ✓
- `BOOLEAN` type in PostgreSQL vs `INTEGER` (0/1) in SQLite
- Need to run `go mod tidy` after changing drivers
- Seed data must be idempotent (ON CONFLICT DO NOTHING) to survive restarts
- Usage log for streaming responses will have token_count = 0 initially
- Cursor-Pro-Opus "Copilot" model slot assumed to be claude-opus-4-6-thinking via GHCP

## Verification

After implementation:
```bash
# 1. Start PostgreSQL and create database
psql -c "CREATE DATABASE apipod;"

# 2. Build and run
go build ./cmd/server && ./server

# 3. Create a user with cursor-pro-sonnet plan
curl -X POST http://localhost:8081/admin/create-key \
  -H "x-admin-secret: <secret>" \
  -H "Content-Type: application/json" \
  -d '{"name": "test", "sub_name": "cursor-pro-sonnet"}'

# 4. Use the API key to make a chat request
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer <api_key>" \
  -H "Content-Type: application/json" \
  -d '{"model": "cursor-pro-sonnet", "messages": [{"role": "user", "content": "hi"}]}'

# 5. Verify usage log in PostgreSQL
psql postgresql://local:local@127.0.0.1/apipod -c "SELECT * FROM usage_logs;"
```

## Status
- [ ] Not started
- [ ] In progress
- [ ] Completed
