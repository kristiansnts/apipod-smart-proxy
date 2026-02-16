package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/rpay/apipod-smart-proxy/internal/config"
	"github.com/rpay/apipod-smart-proxy/internal/database"
	"github.com/rpay/apipod-smart-proxy/internal/middleware"
)

type Handler struct {
	cfg          *config.Config // Added config to the Handler struct
	db           *database.DB
	logger       *log.Logger
	router       *Router
	pools        map[int64]interface{}
	poolsMu      sync.RWMutex
}

func NewHandler(cfg *config.Config, router *Router, db *database.DB, logger *log.Logger) *Handler {
	return &Handler{
		cfg:    cfg, // Initialize config
		db:     db,
		logger: logger,
		router: router,
		pools:  make(map[int64]interface{}),
	}
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "healthy", "service": "apipod-smart-proxy"}`))
}

func (h *Handler) HandleChatCompletion(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(bodyBytes, &req)

	routing, err := h.router.RouteModel(user.SubID, req.Model)
	if err != nil {
		h.logger.Printf("Routing Error: %v", err)
		http.Error(w, `{"error": "Routing failed"}`, http.StatusInternalServerError)
		return
	}

	h.handleNativeUpstream(w, r, routing, user, req.Model, bodyBytes)
}

func (h *Handler) getAntigravityPool(providerID int64) (interface{}, error) {
	return nil, nil
}
