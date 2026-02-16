package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
	"github.com/rpay/apipod-smart-proxy/internal/pool"
)

type Handler struct {
	db           *database.DB
	logger       *log.Logger
	runnerLogger *log.Logger
	router       *Router
	pools        map[int64]*pool.AccountPool
	poolMu       sync.Mutex
}

func NewHandler(router *Router, db *database.DB, logger *log.Logger, runnerLogger *log.Logger) *Handler {
	return &Handler{
		db:           db,
		logger:       logger,
		runnerLogger: runnerLogger,
		router:       router,
		pools:        make(map[int64]*pool.AccountPool),
	}
}

// getPool returns the account pool for a provider, loading from DB if not cached.
func (h *Handler) getPool(providerID int64) *pool.AccountPool {
	h.poolMu.Lock()
	defer h.poolMu.Unlock()

	if p, ok := h.pools[providerID]; ok {
		return p
	}

	accounts, err := h.db.GetAccountsForProvider(uint(providerID))
	if err != nil {
		h.logger.Printf("Failed to load accounts for provider %d: %v", providerID, err)
		return nil
	}

	if len(accounts) == 0 {
		return nil
	}

	p := pool.NewAccountPool()
	for _, acc := range accounts {
		p.Accounts = append(p.Accounts, &pool.Account{
			ID:         acc.ID,
			Email:      acc.Email,
			APIKey:     acc.APIKey,
			LimitType:  acc.LimitType,
			LimitValue: acc.LimitValue,
		})
	}
	h.pools[providerID] = p
	h.logger.Printf("Loaded %d accounts into pool for provider %d", len(accounts), providerID)
	return p
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "healthy", "service": "apipod-smart-proxy"}`))
}

// HandleMessages handles Anthropic Messages API requests (POST /v1/messages).
func (h *Handler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": {"type": "authentication_error", "message": "Unauthorized"}}`, http.StatusUnauthorized)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Printf("Failed to read request body: %v", err)
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Failed to read request body"}}`, http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(bodyBytes, &req)

	routing, err := h.router.RouteModel(user.SubID, req.Model)
	if err != nil {
		h.runnerLogger.Printf("ERROR [routing] model=%s user=%d sub=%d err=%v", req.Model, user.ID, user.SubID, err)
		http.Error(w, `{"error": {"type": "not_found_error", "message": "Routing failed"}}`, http.StatusInternalServerError)
		return
	}

	h.handleNativeUpstreamAnthropic(w, r, routing, user, req.Model, bodyBytes)
}

func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Printf("Failed to read request body: %v", err)
		http.Error(w, `{"error": "Failed to read request body"}`, http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(bodyBytes, &req)

	routing, err := h.router.RouteModel(user.SubID, req.Model)
	if err != nil {
		h.runnerLogger.Printf("ERROR [routing] model=%s user=%d sub=%d err=%v", req.Model, user.ID, user.SubID, err)
		http.Error(w, `{"error": "Routing failed"}`, http.StatusInternalServerError)
		return
	}

	h.handleNativeUpstream(w, r, routing, user, req.Model, bodyBytes)
}
