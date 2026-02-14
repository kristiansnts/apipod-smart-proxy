package proxy

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/rpay/apipod-smart-proxy/internal/database"
)

// UpstreamTarget represents which upstream to use
type UpstreamTarget string

const (
	UpstreamAntigravity UpstreamTarget = "antigravity"
	UpstreamGHCP        UpstreamTarget = "ghcp"
)

// RoutingResult contains the routed model, target upstream, and quota item ID for logging
type RoutingResult struct {
	Model       string
	Upstream    UpstreamTarget
	QuotaItemID int64
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
// Returns the routing result (model, upstream, quota_item_id).
// If the subscription has no quota items, falls back to Antigravity with the original model name.
func (r *Router) RouteModel(subID int64, fallbackModel string) (RoutingResult, error) {
	items, err := r.db.GetQuotaItemsBySubID(subID)
	if err != nil {
		return RoutingResult{}, fmt.Errorf("route model: %w", err)
	}

	if len(items) == 0 {
		// No quota items configured — pass through unchanged
		return RoutingResult{
			Model:    fallbackModel,
			Upstream: UpstreamAntigravity,
		}, nil
	}

	// Weighted random selection
	totalWeight := 0
	for _, item := range items {
		totalWeight += item.PercentageWeight
	}

	roll := r.rand.Intn(totalWeight)
	cumulative := 0
	for _, item := range items {
		cumulative += item.PercentageWeight
		if roll < cumulative {
			return RoutingResult{
				Model:       item.ModelName,
				Upstream:    UpstreamTarget(item.Upstream),
				QuotaItemID: item.QuotaID,
			}, nil
		}
	}

	// Fallback (should never reach here)
	last := items[len(items)-1]
	return RoutingResult{
		Model:       last.ModelName,
		Upstream:    UpstreamTarget(last.Upstream),
		QuotaItemID: last.QuotaID,
	}, nil
}
