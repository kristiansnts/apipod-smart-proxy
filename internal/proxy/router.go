package proxy

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/database"
)

// RoutingResult contains the routed model, target upstream details, and quota item ID for logging
type RoutingResult struct {
	Model        string
	BaseURL      string
	APIKey       string
	ProviderType string
	QuotaItemID  int64
	ProviderID   int64
	LLMModelID   int64
	RPM          *int
	TPM          *int
	RPD          *int
}

// Router handles DB-driven weighted model routing
type Router struct {
	db   *database.DB
	rand *rand.Rand
}

// NewRouter creates a new smart router backed by the database
func NewRouter(db *database.DB) *Router {
	src := rand.NewSource(time.Now().UnixNano())
	return &Router{
		db:   db,
		rand: rand.New(src),
	}
}

// RouteModel selects a model/upstream for the given subscription using weighted random selection.
// Returns the routing result (model, upstream details, quota_item_id).
func (r *Router) RouteModel(subID int64, fallbackModel string) (RoutingResult, error) {
	items, err := r.db.GetQuotaItemsBySubID(subID)
	if err != nil {
		return RoutingResult{}, fmt.Errorf("route model: %w", err)
	}

	if len(items) == 0 {
		return RoutingResult{}, fmt.Errorf("no quota items configured for sub_id=%d", subID)
	}

	// Weighted random selection
	totalWeight := 0
	for _, item := range items {
		totalWeight += item.PercentageWeight
	}

	if totalWeight <= 0 {
		return RoutingResult{}, fmt.Errorf("total weight must be greater than 0 for sub_id=%d", subID)
	}

	roll := r.rand.Intn(totalWeight)
	cumulative := 0
	for _, item := range items {
		cumulative += item.PercentageWeight
		if roll < cumulative {
			return RoutingResult{
				Model:        item.ModelName,
				BaseURL:      item.BaseURL,
				APIKey:       item.APIKey,
				ProviderType: item.ProviderType,
				QuotaItemID:  item.QuotaID,
				ProviderID:   item.ProviderID,
				LLMModelID:   item.LLMModelID,
				RPM:          item.RPM,
				TPM:          item.TPM,
				RPD:          item.RPD,
			}, nil
		}
	}

	// Fallback
	last := items[len(items)-1]
	return RoutingResult{
		Model:        last.ModelName,
		BaseURL:      last.BaseURL,
		APIKey:       last.APIKey,
		ProviderType: last.ProviderType,
		QuotaItemID:  last.QuotaID,
		ProviderID:   last.ProviderID,
		LLMModelID:   last.LLMModelID,
		RPM:          last.RPM,
		TPM:          last.TPM,
		RPD:          last.RPD,
	}, nil
}
