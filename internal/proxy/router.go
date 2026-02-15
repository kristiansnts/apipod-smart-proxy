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
}

// Router handles DB-driven weighted model routing with RPM awareness
type Router struct {
	db          *database.DB
	rand        *rand.Rand
	rateLimiter *RateLimiter
}

// NewRouter creates a new smart router backed by the database
func NewRouter(db *database.DB) *Router {
	src := rand.NewSource(time.Now().UnixNano())
	return &Router{
		db:          db,
		rand:        rand.New(src),
		rateLimiter: NewRateLimiter(),
	}
}

// RouteModel selects a model/upstream for the given subscription using weighted random selection with RPM failover.
func (r *Router) RouteModel(subID int64, fallbackModel string) (RoutingResult, error) {
	items, err := r.db.GetQuotaItemsBySubID(subID)
	if err != nil {
		return RoutingResult{}, fmt.Errorf("route model: %w", err)
	}

	if len(items) == 0 {
		return RoutingResult{}, fmt.Errorf("no quota items configured for sub_id=%d", subID)
	}

	// Algorithm: 
	// 1. Sort available items by weight (highest first)
	// 2. Try items in order. If RPM limit reached, try the next one.
	
	// Copy and shuffle/sort based on weights for "Smooth Weighted" feel
	// For simplicity in this iteration, we use Weighted Random selection but with multiple attempts.
	
	remainingItems := make([]database.QuotaItem, len(items))
	copy(remainingItems, items)

	for len(remainingItems) > 0 {
		totalWeight := 0
		for _, item := range remainingItems {
			totalWeight += item.PercentageWeight
		}

		if totalWeight <= 0 {
			// If all remaining have 0 weight, pick first available that isn't rate limited
			for _, item := range remainingItems {
				if r.rateLimiter.Allow(item.QuotaID, item.RPMLimit) {
					return r.toResult(item), nil
				}
			}
			break
		}

		roll := r.rand.Intn(totalWeight)
		cumulative := 0
		selectedIndex := -1
		
		for i, item := range remainingItems {
			cumulative += item.PercentageWeight
			if roll < cumulative {
				selectedIndex = i
				break
			}
		}

		selected := remainingItems[selectedIndex]
		if r.rateLimiter.Allow(selected.QuotaID, selected.RPMLimit) {
			return r.toResult(selected), nil
		}

		// If rate limited, remove this item and try again
		remainingItems = append(remainingItems[:selectedIndex], remainingItems[selectedIndex+1:]...)
	}

	return RoutingResult{}, fmt.Errorf("all configured models reached RPM limit for sub_id=%d", subID)
}

func (r *Router) toResult(item database.QuotaItem) RoutingResult {
	return RoutingResult{
		Model:        item.ModelName,
		BaseURL:      item.BaseURL,
		APIKey:       item.APIKey,
		ProviderType: item.ProviderType,
		QuotaItemID:  item.QuotaID,
	}
}
