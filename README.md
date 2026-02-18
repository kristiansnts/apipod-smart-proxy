# 🐬 Apipod Smart Proxy

A production-ready smart API proxy in Go that orchestrates multiple AI providers including Antigravity, Google AI Studio, OpenAI, and GitHub Copilot, featuring intelligent model routing, authentication, and streaming support.

## ✨ Features

- **🎯 Multi-Provider Orchestration**: Supports Antigravity, Google AI Studio, OpenAI-compatible, and GitHub Copilot endpoints
- **🧠 Smart Model Routing**: Intelligent routing logic based on model availability and provider capabilities
- **📊 Model Limiting**: Pool-based rate limiting with configurable concurrency controls
- **🔐 API Key Authentication**: PostgreSQL-based user management with expiration support
- **💰 Quota Management**: Subscription-based quota tracking and usage monitoring
- **📡 Full Streaming Support**: Server-Sent Events (SSE) with configurable flush intervals
- **👑 Admin Dashboard**: Comprehensive API key generation and user management
- **🚀 Production Ready**: Graceful shutdown, structured logging, health checks, Docker support

## 🏗️ Architecture

```
Client Request
     ↓
API Key Authentication & Authorization
     ↓
Orchestrator (Model Routing & Provider Selection)
     ↓
Pool Manager (Concurrency & Rate Limiting)
     ↓
Upstream Provider Proxy
     ├─ Antigravity Proxy
     ├─ Google AI Studio
     ├─ OpenAI-Compatible
     └─ GitHub Copilot
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
# Server Configuration
PORT=8081

# Database Configuration
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=apipod
POSTGRES_PASSWORD=your-secure-password
POSTGRES_DB=apipod_proxy
POSTGRES_SSLMODE=disable

# Admin secret for creating API keys (min 16 chars)
ADMIN_SECRET=your-secure-admin-secret

# Upstream Provider Configuration
ANTIGRAVITY_URL=http://127.0.0.1:8080
ANTIGRAVITY_KEY=your-antigravity-api-key
GOOGLE_AI_STUDIO_KEY=your-google-ai-studio-key
OPENAI_API_KEY=your-openai-api-key
COPILOT_CLIENT_ID=your-copilot-client-id
COPILOT_CLIENT_SECRET=your-copilot-client-secret

# Model Pool Configuration
MAX_CONCURRENT_REQUESTS=100
MODEL_CONCURRENCY_LIMIT=10

# Logging Configuration
LOG_LEVEL=info
LOG_FORMAT=json

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

### Chat Completions (OpenAI-Compatible)
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

---

### Messages API (Anthropic-Compatible)
```bash
POST /v1/messages
```
Protected by API key authentication.

**Headers:**
- `Authorization`: Bearer <your-api-key> or `x-api-key`: <your-api-key>
- `Content-Type`: application/json

**Request Body (Anthropic-compatible):**
```json
{
  "model": "claude-3-sonnet-20240229",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "max_tokens": 1024
}
```

**Example:**
```bash
curl -X POST http://localhost:8081/v1/messages \
  -H "Authorization: Bearer apk_your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-sonnet-20240229",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 1024
  }'
```

---

### Auth Copilot Tool
```bash
./auth-copilot
```
Standalone tool for GitHub Copilot authentication.

**Usage:**
```bash
cd cmd/auth-copilot
go run main.go
```

This tool helps generate GitHub Copilot access tokens for use with the proxy.

## 🎲 Smart Routing Logic

The proxy uses intelligent routing based on:
- Model availability across providers
- Provider account configuration and health
- Concurrency limits and rate limiting
- Subscription tier entitlements

Requests are automatically routed to the most appropriate upstream provider based on configured rules and real-time availability.

## 🐳 Docker Deployment

### Build and Run with Docker

```bash
# Build image
docker build -t apipod-smart-proxy .

# Run container (PostgreSQL required externally)
docker run -d \
  --name apipod-proxy \
  -p 8081:8081 \
  -e POSTGRES_HOST=your-postgres-host \
  -e POSTGRES_PORT=5432 \
  -e POSTGRES_USER=apipod \
  -e POSTGRES_PASSWORD=your-secure-password \
  -e POSTGRES_DB=apipod_proxy \
  -e ADMIN_SECRET=your-admin-secret \
  -e ANTIGRAVITY_URL=http://host.docker.internal:8080 \
  -e ANTIGRAVITY_KEY=your-antigravity-key \
  -e GOOGLE_AI_STUDIO_KEY=your-google-key \
  -e OPENAI_API_KEY=your-openai-key \
  apipod-smart-proxy
```

### Docker Compose with PostgreSQL

```bash
# Start all services (proxy + PostgreSQL)
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## 📁 Project Structure

```
apipod-smart-proxy/
├── cmd/
│   ├── server/main.go                 # Main proxy server entry point
│   └── auth-copilot/main.go           # GitHub Copilot auth endpoint
├── internal/
│   ├── config/config.go               # Environment configuration
│   ├── database/
│   │   ├── database.go                # PostgreSQL connection
│   │   ├── user.go                    # User model & queries
│   │   ├── provider_account.go        # Provider account management
│   │   ├── subscription.go            # Subscription management
│   │   ├── quota_item.go              # Quota tracking
│   │   └── usage_log.go               # Usage logging
│   ├── middleware/
│   │   ├── auth.go                    # API key authentication
│   │   └── logging.go                 # Request/response logging
│   ├── orchestrator/
│   │   ├── orchestrator.go            # Model routing logic
│   │   └── prompt.go                  # Prompt processing
│   ├── pool/
│   │   ├── pool.go                    # Connection pool management
│   │   └── model_limiter.go           # Model rate limiting
│   ├── proxy/
│   │   ├── models.go                  # OpenAI-compatible structs
│   │   ├── router.go                  # Smart routing logic
│   │   ├── handler.go                 # Reverse proxy + streaming
│   │   └── native_handler.go          # Native request handling
│   └── admin/handler.go               # Admin endpoint
├── internal/upstream/
│   ├── antigravity/
│   │   ├── client.go                  # Antigravity client
│   │   └── response.go                # Response handling
│   ├── googleaistudio/
│   │   ├── client.go                  # Google AI Studio client
│   │   ├── convert.go                 # Request/response conversion
│   │   └── response.go                # Response handling
│   ├── openaicompat/
│   │   ├── client.go                  # OpenAI-compatible client
│   │   └── response.go                # Response handling
│   ├── copilot/
│   │   ├── client.go                  # GitHub Copilot client
│   │   └── transform.go               # Request transformation
│   └── anthropiccompat/convert.go     # Anthropic compatibility
├── pkg/keygen/keygen.go               # API key generation
├── go.mod                             # Go dependencies
├── .env.example                       # Config template
├── Dockerfile                         # Container image
├── docker-compose.yml                 # Docker Compose setup
└── README.md                          # This file
```

## 🔒 Security Notes

1. **Admin Secret**: Must be at least 16 characters. Use a strong, random secret in production.
2. **API Keys**: Stored encrypted at rest in PostgreSQL. Ensure proper database security.
3. **HTTPS**: Use a reverse proxy (nginx/caddy) for HTTPS in production.
4. **Rate Limiting**: Implemented through pool-based concurrency controls.
5. **Database Security**: Use PostgreSQL with SSL and proper access controls.

## 🐛 Troubleshooting

### Database connection error
- Verify PostgreSQL is running and accessible
- Check database connection string in environment variables
- Ensure proper PostgreSQL user permissions

### Streaming not working
- Ensure you're using `curl -N` flag
- Check that `Content-Type: text/event-stream` is in response headers
- Verify proxy configuration supports streaming

### 401 Unauthorized
- Verify your API key is correct and active
- Check that the key hasn't expired
- Ensure `Authorization: Bearer <key>` header is present

### 403 Forbidden
- API key may be inactive, expired, or lacks permissions
- For admin endpoint, verify `x-admin-secret` matches your `.env`

## 📊 Monitoring

### Health Check

```bash
# Check if service is running
curl http://localhost:8081/health
```

### Database Inspection

```bash
# Connect to PostgreSQL database
psql postgresql://apipod:password@localhost/apipod_proxy

# List all API keys
SELECT api_key, name, tier, active, created_at, expires_at FROM users;

# Check active users
SELECT COUNT(*) FROM users WHERE active = true;
```

## 🚀 Production Deployment

### Recommended Setup

1. **Reverse Proxy**: Use nginx or caddy for HTTPS termination
2. **Database Backups**: Implement PostgreSQL backup strategy
3. **Secrets Management**: Use environment variables or secret management system
4. **Monitoring**: Set up health check monitoring and metrics
5. **Logging**: Configure structured logging with rotation
6. **Scaling**: Use connection pooling and load balancing for high traffic

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
