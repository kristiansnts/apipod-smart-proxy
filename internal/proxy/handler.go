package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/metrics"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
	"github.com/rpay/apipod-smart-proxy/internal/orchestrator"
	"github.com/rpay/apipod-smart-proxy/internal/pool"
	"github.com/rpay/apipod-smart-proxy/internal/tools"
)

type Handler struct {
	db             *database.DB
	logger         *log.Logger
	runnerLogger   *log.Logger
	router         *Router
	pools          map[int64]*pool.AccountPool
	poolMu         sync.Mutex
	modelLimiter   *pool.ModelLimiter
	orchestrator   *orchestrator.Orchestrator
	toolExecutor   *tools.Executor
	rateLimiter    *RateLimiter
	usageCommitter *UsageCommitter
	metrics        *metrics.Metrics
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func NewHandler(router *Router, db *database.DB, logger *log.Logger, runnerLogger *log.Logger, modelLimiter *pool.ModelLimiter, usageCommitter *UsageCommitter, m *metrics.Metrics) *Handler {
	return &Handler{
		db:             db,
		logger:         logger,
		runnerLogger:   runnerLogger,
		router:         router,
		pools:          make(map[int64]*pool.AccountPool),
		modelLimiter:   modelLimiter,
		orchestrator:   orchestrator.New(runnerLogger),
		toolExecutor:   tools.NewExecutor(runnerLogger),
		rateLimiter:    NewRateLimiter(),
		usageCommitter: usageCommitter,
		metrics:        m,
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
			ID:     acc.ID,
			Email:  acc.Email,
			APIKey: acc.APIKey,
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

// enforceRuntimeConfig checks rate limits, daily quota, and model access.
// Returns the RuntimeConfig if allowed, or writes error response and returns nil.
func (h *Handler) enforceRuntimeConfig(w http.ResponseWriter, r *http.Request, model string) *config.RuntimeConfig {
	cfg := middleware.GetConfigFromContext(r.Context())
	if cfg == nil {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return nil
	}

	// Rate limit check
	if !h.rateLimiter.AllowRequest(cfg.OrgID, cfg.RateLimitRPM) {
		h.runnerLogger.Printf("RATE_LIMITED [rpm] org=%d rpm=%d", cfg.OrgID, cfg.RateLimitRPM)
		http.Error(w, `{"error": "Rate limit exceeded"}`, http.StatusTooManyRequests)
		return nil
	}

	// Daily quota check
	if !h.rateLimiter.CheckDailyQuota(cfg.OrgID, cfg.DailyUsed, cfg.DailyQuota) {
		h.runnerLogger.Printf("RATE_LIMITED [daily] org=%d used=%d cap=%d", cfg.OrgID, cfg.DailyUsed, cfg.DailyQuota)
		http.Error(w, `{"error": "Daily request limit reached"}`, http.StatusTooManyRequests)
		return nil
	}

	// Model access check
	if len(cfg.AllowedModels) > 0 {
		allowed := false
		for _, m := range cfg.AllowedModels {
			if m == model {
				allowed = true
				break
			}
		}
		if !allowed {
			h.runnerLogger.Printf("DENIED [model] org=%d model=%s", cfg.OrgID, model)
			http.Error(w, `{"error": "Model not allowed on your plan"}`, http.StatusForbidden)
			return nil
		}
	}

	return cfg
}

// injectBYOKKey overrides routing.APIKey with user's own key if in BYOK mode.
// Returns false and writes error if BYOK user has no key for the provider.
func (h *Handler) injectBYOKKey(w http.ResponseWriter, cfg *config.RuntimeConfig, routing *RoutingResult) bool {
	if cfg.Mode != "byok" {
		return true // platform mode, nothing to inject
	}

	for _, uk := range cfg.UpstreamKeys {
		if uk.ProviderID == uint(routing.ProviderID) {
			routing.APIKey = uk.APIKey
			return true
		}
	}

	// BYOK user but no key for this provider
	http.Error(w, `{"error": "No provider key configured. Add one in your dashboard."}`, http.StatusForbidden)
	return false
}

// routeBYOK builds a RoutingResult directly from the user's active model config.
// BYOK users skip the weighted router — they use their single selected model.
func (h *Handler) routeBYOK(w http.ResponseWriter, cfg *config.RuntimeConfig) (RoutingResult, bool) {
	if cfg.ActiveModel == nil {
		http.Error(w, `{"error": "No model selected. Choose a model in your dashboard."}`, http.StatusForbidden)
		return RoutingResult{}, false
	}

	if cfg.ActiveModel.APIKey == "" {
		http.Error(w, `{"error": "No provider key for your selected model. Add one in your dashboard."}`, http.StatusForbidden)
		return RoutingResult{}, false
	}

	return RoutingResult{
		Model:        cfg.ActiveModel.ModelName,
		BaseURL:      cfg.ActiveModel.BaseURL,
		APIKey:       cfg.ActiveModel.APIKey,
		ProviderType: cfg.ActiveModel.ProviderType,
		ProviderID:   int64(cfg.ActiveModel.ProviderID),
	}, true
}

// HandleMessages handles Anthropic Messages API requests (POST /v1/messages).
func (h *Handler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	cacheHit := false
	defer func() {
		if h.metrics != nil {
			h.metrics.Record(time.Since(start).Milliseconds(), rec.status < 400, cacheHit)
		}
	}()
	w = rec

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil || len(bodyBytes) == 0 {
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Failed to read request body"}}`, http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, `{"error": {"type": "invalid_request_error", "message": "Invalid JSON in request body"}}`, http.StatusBadRequest)
		return
	}

	// Enforce rate limits + model access
	cfg := h.enforceRuntimeConfig(w, r, req.Model)
	if cfg == nil {
		return
	}

	// Route: BYOK uses active model directly, platform uses weighted router
	var routing RoutingResult
	if cfg.Mode == "byok" {
		var ok bool
		routing, ok = h.routeBYOK(w, cfg)
		if !ok {
			return
		}
	} else {
		var err error
		routing, err = h.router.RouteModel(cfg.SubID, req.Model)
		if err != nil {
			h.runnerLogger.Printf("ERROR [routing] model=%s org=%d err=%v", req.Model, cfg.OrgID, err)
			http.Error(w, `{"error": {"type": "not_found_error", "message": "Routing failed"}}`, http.StatusInternalServerError)
			return
		}
	}

	// Model rate limiting
	if routing.LLMModelID > 0 {
		h.modelLimiter.SetLimits(routing.LLMModelID, routing.RPM, routing.TPM, routing.RPD)
		if !h.modelLimiter.AllowRequest(routing.LLMModelID) {
			http.Error(w, `{"error": {"type": "rate_limit_error", "message": "Model rate limit exceeded"}}`, http.StatusTooManyRequests)
			return
		}
	}

	// Build legacy User object for native_handler compatibility
	user := &database.User{
		ID:       fmt.Sprintf("%d", cfg.OrgID),
		Username: fmt.Sprintf("org_%d", cfg.OrgID),
	}

	inTokens, outTokens, cacheHit := h.handleNativeUpstreamAnthropic(w, r, routing, user, req.Model, bodyBytes)

	// Async usage commit (non-blocking)
	if h.usageCommitter != nil {
		h.usageCommitter.CommitAsync(cfg.OrgID, cfg.APIKeyID, routing.Model, cfg.Mode, inTokens, outTokens, rec.status, time.Since(start).Milliseconds(), cacheHit)
	}
}

func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	cacheHit := false
	defer func() {
		if h.metrics != nil {
			h.metrics.Record(time.Since(start).Milliseconds(), rec.status < 400, cacheHit)
		}
	}()
	w = rec

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil || len(bodyBytes) == 0 {
		http.Error(w, `{"error": "Failed to read request body"}`, http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, `{"error": "Invalid JSON in request body"}`, http.StatusBadRequest)
		return
	}

	// Enforce rate limits + model access
	cfg := h.enforceRuntimeConfig(w, r, req.Model)
	if cfg == nil {
		return
	}

	// Route: BYOK uses active model directly, platform uses weighted router
	var routing RoutingResult
	if cfg.Mode == "byok" {
		var ok bool
		routing, ok = h.routeBYOK(w, cfg)
		if !ok {
			return
		}
	} else {
		var err error
		routing, err = h.router.RouteModel(cfg.SubID, req.Model)
		if err != nil {
			h.runnerLogger.Printf("ERROR [routing] model=%s org=%d err=%v", req.Model, cfg.OrgID, err)
			http.Error(w, `{"error": "Routing failed"}`, http.StatusInternalServerError)
			return
		}
	}

	// Model rate limiting
	if routing.LLMModelID > 0 {
		h.modelLimiter.SetLimits(routing.LLMModelID, routing.RPM, routing.TPM, routing.RPD)
		if !h.modelLimiter.AllowRequest(routing.LLMModelID) {
			http.Error(w, `{"error": "Model rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
	}

	// Build legacy User object for native_handler compatibility
	user := &database.User{
		ID:       fmt.Sprintf("%d", cfg.OrgID),
		Username: fmt.Sprintf("org_%d", cfg.OrgID),
	}

	inTokens, outTokens, cacheHit := h.handleNativeUpstream(w, r, routing, user, req.Model, bodyBytes)

	// Async usage commit (non-blocking)
	if h.usageCommitter != nil {
		h.usageCommitter.CommitAsync(cfg.OrgID, cfg.APIKeyID, routing.Model, cfg.Mode, inTokens, outTokens, rec.status, time.Since(start).Milliseconds(), cacheHit)
	}
}
