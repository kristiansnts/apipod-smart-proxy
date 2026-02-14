# 🐬 Apipod Smart Proxy

A production-ready smart API proxy in Go that sits between clients and an upstream Antigravity Proxy, featuring intelligent model routing, authentication, and streaming support.

## ✨ Features

- **🎯 Smart Routing**: Automatically routes `cursor-pro-sonnet` requests with 20/80 distribution
  - 20% → `claude-sonnet-4-5` (premium)
  - 80% → `gemini-3-flash` (budget)
- **🔐 API Key Authentication**: SQLite-based user management with expiration support
- **📡 Streaming Support**: Full SSE (Server-Sent Events) with 100ms flush interval for real-time chat
- **👑 Admin Panel**: Secure API key generation endpoint
- **🚀 Production Ready**: Graceful shutdown, logging, health checks, Docker support

## 🏗️ Architecture

```
Client Request
     ↓
API Key Auth Middleware
     ↓
Smart Router (20/80 split)
     ↓
Reverse Proxy (with streaming)
     ↓
Antigravity Proxy
```

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- SQLite (pure Go implementation, no CGO required)

### Quick Start

1. **Clone and setup**
   ```bash
   git clone <your-repo-url>
   cd apipod-smart-proxy
   ```

2. **Install dependencies**
   ```bash
   go mod tidy
   ```

3. **Configure environment**
   ```bash
   cp .env.example .env
   # Edit .env with your actual values
   ```

4. **Run the server**
   ```bash
   go run cmd/server/main.go
   ```

   Or build and run:
   ```bash
   go build -o apipod-proxy cmd/server/main.go
   ./apipod-proxy
   ```

## 🔧 Configuration

Create a `.env` file with the following variables:

```bash
# Upstream Antigravity Proxy URL
ANTIGRAVITY_URL=http://127.0.0.1:8080

# Antigravity API Key
ANTIGRAVITY_KEY=your-antigravity-api-key

# Admin secret for creating API keys (min 16 chars)
ADMIN_SECRET=your-secure-admin-secret

# Server port
PORT=8081

# SQLite database path
DATABASE_PATH=./data/proxy.db
```

## 📚 API Endpoints

### Health Check
```bash
GET /health
```
No authentication required. Returns server status.

**Example:**
```bash
curl http://localhost:8081/health
```

**Response:**
```json
{
  "status": "healthy",
  "service": "apipod-smart-proxy"
}
```

---

### Create API Key (Admin)
```bash
POST /admin/create-key
```
Protected by `x-admin-secret` header.

**Headers:**
- `x-admin-secret`: Your admin secret from `.env`
- `Content-Type`: application/json

**Request Body:**
```json
{
  "name": "user-name",
  "tier": "elite",
  "expires_in": 30
}
```

**Parameters:**
- `name` (required): User name
- `tier` (optional): User tier (default: "elite")
- `expires_in` (optional): Days until expiration

**Example:**
```bash
curl -X POST http://localhost:8081/admin/create-key \
  -H "x-admin-secret: your-admin-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-user",
    "tier": "elite",
    "expires_in": 30
  }'
```

**Response:**
```json
{
  "api_key": "apk_abc123...",
  "name": "test-user",
  "tier": "elite",
  "created_at": "2024-01-01T00:00:00Z",
  "expires_at": "2024-01-31T00:00:00Z"
}
```

⚠️ **Important**: The API key is only shown once! Save it securely.

---

### Chat Completions
```bash
POST /v1/chat/completions
```
Protected by API key authentication.

**Headers:**
- `Authorization`: Bearer <your-api-key>
- `Content-Type`: application/json

**Request Body (OpenAI-compatible):**
```json
{
  "model": "cursor-pro-sonnet",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "stream": true,
  "temperature": 0.7
}
```

**Example (Non-streaming):**
```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer apk_your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "cursor-pro-sonnet",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

**Example (Streaming with SSE):**
```bash
curl -N -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer apk_your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "cursor-pro-sonnet",
    "messages": [{"role": "user", "content": "Tell me a story"}],
    "stream": true
  }'
```

> **Note**: Use `-N` flag with curl to disable buffering for streaming responses.

## 🎲 Smart Routing Logic

When a client requests `cursor-pro-sonnet`, the proxy applies smart routing:

```
cursor-pro-sonnet
       ↓
Random selection:
  ├─ 20% → claude-sonnet-4-5  (premium)
  └─ 80% → gemini-3-flash     (budget)
```

Other models pass through unchanged.

### Testing Distribution

Run 20 requests to verify the 20/80 split:

```bash
for i in {1..20}; do
  curl -s -X POST http://localhost:8081/v1/chat/completions \
    -H "Authorization: Bearer apk_your-key" \
    -H "Content-Type: application/json" \
    -d '{"model": "cursor-pro-sonnet", "messages": [{"role": "user", "content": "Hi"}], "stream": false}' \
    > /dev/null
done

# Check server logs:
# You should see ~4 requests routed to claude-sonnet-4-5
# and ~16 requests routed to gemini-3-flash
```

## 🐳 Docker Deployment

### Build and Run with Docker

```bash
# Build image
docker build -t apipod-smart-proxy .

# Run container
docker run -d \
  --name apipod-proxy \
  -p 8081:8081 \
  -v $(pwd)/data:/root/data \
  -e ANTIGRAVITY_URL=http://host.docker.internal:8080 \
  -e ANTIGRAVITY_KEY=your-key \
  -e ADMIN_SECRET=your-secret \
  apipod-smart-proxy
```

### Docker Compose

```bash
# Start services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## 📁 Project Structure

```
apipod-smart-proxy/
├── cmd/server/main.go              # Entry point
├── internal/
│   ├── config/config.go            # Environment configuration
│   ├── database/
│   │   ├── database.go             # SQLite connection
│   │   └── user.go                 # User model & queries
│   ├── middleware/
│   │   ├── auth.go                 # API key authentication
│   │   └── logging.go              # Request/response logging
│   ├── proxy/
│   │   ├── models.go               # OpenAI-compatible structs
│   │   ├── router.go               # Smart routing (20/80)
│   │   └── handler.go              # Reverse proxy + streaming
│   └── admin/handler.go            # Admin endpoint
├── pkg/keygen/keygen.go            # API key generation
├── go.mod                          # Go dependencies
├── .env.example                    # Config template
├── Dockerfile                      # Container image
└── README.md                       # This file
```

## 🔒 Security Notes

1. **Admin Secret**: Must be at least 16 characters. Use a strong, random secret in production.
2. **API Keys**: Stored in plaintext in SQLite. For enhanced security, consider adding encryption at rest.
3. **HTTPS**: Use a reverse proxy (nginx/caddy) for HTTPS in production.
4. **Rate Limiting**: Not implemented yet. Consider adding rate limiting per API key for production use.

## 🐛 Troubleshooting

### Database locked error
Enable WAL mode (already enabled by default):
```sql
PRAGMA journal_mode=WAL;
```

### Streaming not working
- Ensure you're using `curl -N` flag
- Check that `Content-Type: text/event-stream` is in response headers
- Verify `FlushInterval` is set to 100ms in proxy handler

### 401 Unauthorized
- Verify your API key is correct
- Check that the key hasn't expired
- Ensure `Authorization: Bearer <key>` header is present

### 403 Forbidden
- API key may be inactive or expired
- For admin endpoint, verify `x-admin-secret` matches your `.env`

## 📊 Monitoring

### Health Check

```bash
# Check if service is running
curl http://localhost:8081/health
```

### Database Inspection

```bash
# Connect to SQLite database
sqlite3 data/proxy.db

# List all API keys
SELECT api_key, name, tier, active, created_at, expires_at FROM users;

# Check active users
SELECT COUNT(*) FROM users WHERE active = 1;
```

## 🚀 Production Deployment

### Recommended Setup

1. **Reverse Proxy**: Use nginx or caddy for HTTPS
2. **Database Backups**: Regularly backup `data/proxy.db`
3. **Secrets Management**: Use environment variables, not `.env` files
4. **Monitoring**: Set up health check monitoring
5. **Logging**: Configure log rotation and aggregation
6. **Scaling**: For high traffic, consider PostgreSQL instead of SQLite

### Example Nginx Configuration

```nginx
server {
    listen 443 ssl;
    server_name api.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;  # Important for streaming!
    }
}
```

## 📝 License

MIT License - Feel free to use in your projects!

## 🤝 Contributing

Contributions welcome! Please open an issue or PR.

## 📧 Support

For issues and questions, please open a GitHub issue.

---

**Built with ❤️ using Go and modernc.org/sqlite**
