# Plan: Apipod Smart Proxy - Go Implementation

## Objective
Build a production-ready smart API proxy in Go that sits between clients and an upstream Antigravity Proxy, featuring:
- **Smart routing**: 20% cursor-pro-sonnet → claude-sonnet-4-5, 80% → gemini-3-flash
- **Authentication**: SQLite-based API key validation
- **Streaming support**: SSE (Server-Sent Events) with 100ms flush interval
- **Admin panel**: Secure API key generation endpoint

## Token Estimate
**Estimated: 45,000-50,000 tokens** (well within budget with 20K+ buffer)

Breakdown:
- Foundation setup: ~8,000 tokens
- Core components: ~10,000 tokens
- Proxy logic with streaming: ~15,000 tokens
- Integration & main server: ~8,000 tokens
- Documentation & Docker: ~7,000 tokens

## Project Structure

```
apipod-smart-proxy/
├── cmd/server/main.go              # Entry point, server initialization
├── internal/
│   ├── config/config.go            # Environment configuration
│   ├── database/
│   │   ├── database.go             # SQLite connection & schema
│   │   └── user.go                 # User model & queries
│   ├── middleware/
│   │   ├── auth.go                 # API key authentication
│   │   └── logging.go              # Request/response logging
│   ├── proxy/
│   │   ├── models.go               # OpenAI-compatible structs
│   │   ├── router.go               # Smart routing (20/80 split)
│   │   └── handler.go              # Reverse proxy + streaming
│   └── admin/handler.go            # Admin endpoint
├── pkg/keygen/keygen.go            # API key generation
├── go.mod                          # Dependencies
├── .env.example                    # Config template
├── .gitignore                      # Git ignore rules
├── README.md                       # Documentation
├── Dockerfile                      # Container image
└── docker-compose.yml              # Development setup
```

## Dependencies (go.mod)

```go
module github.com/yourusername/apipod-smart-proxy

go 1.21

require (
    github.com/joho/godotenv v1.5.1      // .env loading
    modernc.org/sqlite v1.28.0           // Pure Go SQLite (no CGO)
)
```

## Implementation Steps

### Phase 1: Foundation & Setup
1. **Initialize Go module**
   - `go mod init github.com/yourusername/apipod-smart-proxy`
   - Create directory structure
   - Set up [.gitignore](.gitignore)

2. **Configuration layer** ([internal/config/config.go](internal/config/config.go))
   - Load from `.env` using godotenv
   - Struct: `AntigravityURL`, `AntigravityKey`, `AdminSecret`, `Port`, `DatabasePath`
   - Validation with clear error messages

3. **Key generation utility** ([pkg/keygen/keygen.go](pkg/keygen/keygen.go))
   - Use `crypto/rand` for 32 secure random bytes
   - Base64 URL-safe encoding
   - Prefix with `apk_` → result: `apk_abc123...` (48 chars)

4. **Database layer** ([internal/database/](internal/database/))
   - [database.go](internal/database/database.go): Connection + schema initialization
   - [user.go](internal/database/user.go): User model + CRUD operations
   - Schema:
     ```sql
     CREATE TABLE IF NOT EXISTS users (
         id INTEGER PRIMARY KEY AUTOINCREMENT,
         api_key TEXT UNIQUE NOT NULL,
         name TEXT,
         tier TEXT DEFAULT 'elite',
         active INTEGER DEFAULT 1,
         created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
         expires_at DATETIME
     );
     ```
   - Methods: `CreateUser()`, `GetUserByAPIKey()`, `IsValidAPIKey()`

### Phase 2: Core Components
5. **Authentication middleware** ([internal/middleware/auth.go](internal/middleware/auth.go))
   - Extract `Authorization: Bearer <key>` header
   - Query database for API key
   - Validate: `active=1` and (`expires_at` is NULL or > NOW())
   - Return 401/403 if invalid
   - Store user in request context

6. **Logging middleware** ([internal/middleware/logging.go](internal/middleware/logging.go))
   - Log: method, path, status code, duration
   - Use structured logging format

7. **Admin handler** ([internal/admin/handler.go](internal/admin/handler.go))
   - POST `/admin/create-key`
   - Validate `x-admin-secret` header
   - JSON request: `{name, tier?, expires_in?}`
   - Generate API key, insert to DB
   - Return key in response (only shown once)

### Phase 3: Proxy Logic (Critical)
8. **Proxy models** ([internal/proxy/models.go](internal/proxy/models.go))
   - `ChatCompletionRequest` struct (OpenAI-compatible)
   - Fields: `Model`, `Messages`, `Stream`, `Temperature`, etc.

9. **Smart router** ([internal/proxy/router.go](internal/proxy/router.go))
   - Function: `RouteModel(requestedModel string) string`
   - Logic:
     ```go
     if requestedModel == "cursor-pro-sonnet" {
         if rand.Float64() < 0.20 {
             return "claude-sonnet-4-5"  // 20% premium
         }
         return "gemini-3-flash"         // 80% budget
     }
     return requestedModel  // passthrough
     ```

10. **Proxy handler with streaming** ([internal/proxy/handler.go](internal/proxy/handler.go))
    - POST `/v1/chat/completions`
    - Parse request body → apply smart routing → modify model field
    - Create reverse proxy to `ANTIGRAVITY_URL`
    - Add header: `x-api-key: ANTIGRAVITY_KEY`
    - **Streaming implementation**:
      - Check `stream: true` in request
      - Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`
      - Use `http.Flusher` interface
      - Read upstream response in chunks (4KB buffer)
      - Write + flush every 100ms for smooth streaming
    - Preserve all headers except `Authorization`
    - Log model routing decisions

### Phase 4: Integration
11. **Main server** ([cmd/server/main.go](cmd/server/main.go))
    - Load config from `.env`
    - Initialize database + run migrations
    - Wire components: handlers, middleware, router
    - Setup routes:
      ```go
      POST /v1/chat/completions  → [auth] → proxyHandler
      POST /admin/create-key     → adminHandler (with secret check)
      GET  /health               → healthHandler (no auth)
      ```
    - Start HTTP server with graceful shutdown (SIGINT/SIGTERM)
    - Timeouts: ReadTimeout 15s, WriteTimeout 15s, IdleTimeout 60s

### Phase 5: Production Readiness
12. **Documentation**
    - [.env.example](.env.example): Template with all required variables
    - [README.md](README.md): Setup instructions, testing examples, deployment guide

13. **Docker support**
    - [Dockerfile](Dockerfile): Multi-stage build (golang:1.21-alpine → alpine:latest)
    - [docker-compose.yml](docker-compose.yml): Development environment with volume mounts

14. **Testing & validation**
    - Manual testing with curl commands
    - Verify streaming works with `-N` flag
    - Test authentication flow
    - Validate 20/80 routing distribution

## Critical Files & Existing Utilities

**Files to create** (in order of dependency):
1. [go.mod](go.mod) - Module definition
2. [internal/config/config.go](internal/config/config.go) - Configuration foundation
3. [pkg/keygen/keygen.go](pkg/keygen/keygen.go) - Key generation
4. [internal/database/database.go](internal/database/database.go) - DB setup
5. [internal/database/user.go](internal/database/user.go) - User operations
6. [internal/middleware/auth.go](internal/middleware/auth.go) - Authentication
7. [internal/proxy/router.go](internal/proxy/router.go) - Smart routing
8. [internal/proxy/handler.go](internal/proxy/handler.go) - Proxy + streaming
9. [cmd/server/main.go](cmd/server/main.go) - Main entry point

**No existing utilities** - This is a greenfield project starting from an empty directory.

## Key Implementation Details

### Database Schema with Indices
```sql
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key TEXT UNIQUE NOT NULL,
    name TEXT,
    tier TEXT DEFAULT 'elite',
    active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_api_key ON users(api_key);
CREATE INDEX IF NOT EXISTS idx_active ON users(active);
```

### Streaming SSE Implementation Pattern
```go
func streamResponse(w http.ResponseWriter, upstreamResp *http.Response) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming not supported", 500)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    buf := make([]byte, 4096)
    for {
        n, err := upstreamResp.Body.Read(buf)
        if n > 0 {
            w.Write(buf[:n])
            flusher.Flush()
        }
        if err == io.EOF {
            break
        }
        <-ticker.C
    }
}
```

### Environment Configuration (.env)
```bash
ANTIGRAVITY_URL=http://127.0.0.1:8080
ANTIGRAVITY_KEY=your-antigravity-api-key-here
ADMIN_SECRET=change-this-secret-in-production
PORT=8081
DATABASE_PATH=./data/proxy.db
```

## Verification & Testing

### Step 1: Initialize and run server
```bash
# Install dependencies
go mod tidy

# Run server
go run cmd/server/main.go

# Or build and run
go build -o apipod-proxy cmd/server/main.go
./apipod-proxy
```

### Step 2: Create API key
```bash
curl -X POST http://localhost:8081/admin/create-key \
  -H "x-admin-secret: your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{"name": "test-user", "tier": "elite"}'

# Response: {"api_key": "apk_...", "name": "test-user", ...}
```

### Step 3: Test smart routing (non-streaming)
```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer apk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "cursor-pro-sonnet",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### Step 4: Test streaming (SSE)
```bash
# -N flag disables buffering for streaming
curl -N -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer apk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "cursor-pro-sonnet",
    "messages": [{"role": "user", "content": "Tell me a story"}],
    "stream": true
  }'

# Should see word-by-word streaming output
```

### Step 5: Verify 20/80 distribution
```bash
# Run multiple requests and check server logs
for i in {1..20}; do
  curl -s -X POST http://localhost:8081/v1/chat/completions \
    -H "Authorization: Bearer apk_..." \
    -H "Content-Type: application/json" \
    -d '{"model": "cursor-pro-sonnet", "messages": [{"role": "user", "content": "Hi"}], "stream": false}' \
    > /dev/null
done

# Check logs for routing: ~4 → claude-sonnet-4-5, ~16 → gemini-3-flash
```

### Step 6: Test authentication
```bash
# Should return 401 Unauthorized
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": []}'

# Should return 403 Forbidden (invalid key)
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer invalid_key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": []}'
```

## Risks & Considerations

1. **Streaming complexity**: Requires careful buffer management
   - ✅ Mitigation: Use `http.Flusher` with 100ms ticker

2. **SQLite write contention**: May have locking under high load
   - ✅ Mitigation: Enable WAL mode, acceptable for MVP

3. **API key security**: Stored in plaintext
   - ⚠️ Acceptable for MVP, add hashing later if needed

4. **No rate limiting**: Could be abused
   - ⚠️ Future enhancement, not blocking for initial version

5. **Upstream failures**: Need graceful error handling
   - ✅ Mitigation: Proper timeout configuration and error propagation

## Production Deployment Notes

- Use proper secrets management (not .env files)
- Set up database backups (copy SQLite file regularly)
- Configure reverse proxy (nginx/caddy) for HTTPS
- Enable database WAL mode for better concurrency
- Monitor health endpoint for uptime
- Consider migrating to PostgreSQL if scaling beyond 100 req/s

## Status
- [ ] Not started
- [ ] In progress
- [ ] Completed
