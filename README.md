# 🐬 Apipod Smart Proxy

A lightweight API proxy in Go that routes AI requests to multiple upstream providers. It handles authentication, weighted model routing, rate limiting, and streaming — all configured via a shared PostgreSQL database.

## ✨ Features

- **🎯 Multi-Provider Routing**: Routes requests to Antigravity, Google AI Studio, OpenAI, NVIDIA NIM, and OpenRouter
- **🧠 Weighted Model Selection**: Database-driven weighted random routing across providers and models
- **📊 Rate Limiting**: Per-model RPM/TPM/RPD limits with pool-based concurrency controls
- **🔐 Token Authentication**: Validates API tokens against a shared PostgreSQL database
- **📡 Full Streaming Support**: Server-Sent Events (SSE) passthrough for real-time responses
- **🔧 Tool Execution**: Built-in tool execution system for agentic workflows
- **🚀 Production Ready**: Graceful shutdown, structured logging, health checks, Docker support

## 🏗️ Architecture

```
Client Request (OpenAI / Anthropic format)
     ↓
API Token Authentication (shared DB)
     ↓
Smart Router (weighted model selection per subscription)
     ↓
Rate Limiter (RPM / TPM / RPD per model)
     ↓
Upstream Provider
     ├─ Antigravity
     ├─ Google AI Studio
     ├─ OpenAI
     ├─ NVIDIA NIM
     └─ OpenRouter
```

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- PostgreSQL

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

# PostgreSQL Database URL
DATABASE_URL=postgresql://apipod:your-secure-password@localhost:5432/apipod_proxy?sslmode=disable
```

> Supported providers: **Antigravity**, **Google AI Studio**, **OpenAI**, **NVIDIA NIM**, **OpenRouter**

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

## 🎲 Smart Routing Logic

The proxy selects an upstream provider using **weighted random selection** based on the user's subscription plan:

1. User's subscription (`sub_id`) maps to quota items in the database
2. Each quota item links a model + provider with a percentage weight
3. A weighted random roll picks which model/provider handles the request
4. Rate limits (RPM/TPM/RPD) are enforced per model before forwarding

All provider API keys and base URLs are stored in the `providers` table — no hardcoded credentials.

## 🐳 Docker Deployment

### Build and Run with Docker

```bash
# Build image
docker build -t apipod-smart-proxy .

# Run container (PostgreSQL required externally)
docker run -d \
  --name apipod-proxy \
  -p 8081:8081 \
  -e DATABASE_URL=postgresql://apipod:your-secure-password@your-postgres-host:5432/apipod_proxy?sslmode=disable \
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
│   └── server/main.go                 # Main proxy server entry point
├── internal/
│   ├── config/config.go               # Environment configuration
│   ├── database/
│   │   ├── database.go                # PostgreSQL connection
│   │   ├── user.go                    # User auth queries
│   │   ├── provider_account.go        # Provider account pool
│   │   ├── quota_item.go              # Quota/routing queries
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
│   ├── tools/                         # Tool execution system
│   └── upstream/
│       ├── antigravity/               # Antigravity proxy client
│       ├── googleaistudio/            # Google AI Studio client
│       ├── openaicompat/              # OpenAI / NVIDIA NIM / OpenRouter client
│       └── anthropiccompat/           # Anthropic format conversion
├── go.mod                             # Go dependencies
├── .env.example                       # Config template
├── Dockerfile                         # Container image
├── docker-compose.yml                 # Docker Compose setup
└── README.md                          # This file
```

## 🔒 Security Notes

1. **API Keys**: Validated against the shared PostgreSQL database. Ensure proper database security.
2. **HTTPS**: Use a reverse proxy (nginx/caddy) for HTTPS in production.
3. **Rate Limiting**: Implemented through pool-based concurrency controls.
4. **Database Security**: Use PostgreSQL with SSL and proper access controls.

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

## 📊 Monitoring

```bash
# Health check
curl http://localhost:8081/health
```


## 📝 License

MIT License - Feel free to use in your projects!

## 🤝 Contributing

Contributions welcome! Please open an issue or PR.

## 📧 Support

For issues and questions, please open a GitHub issue.

---

**Built with ❤️ using Go**
