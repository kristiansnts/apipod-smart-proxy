package proxy

import (
	"math/rand"
	"time"
)

// UpstreamTarget represents which upstream to use
type UpstreamTarget string

const (
	UpstreamAntigravity UpstreamTarget = "antigravity"
	UpstreamGHCP        UpstreamTarget = "ghcp"
)

// RoutingResult contains the routed model and target upstream
type RoutingResult struct {
	Model    string
	Upstream UpstreamTarget
}

// Router handles smart model routing
type Router struct {
	rand *rand.Rand
}

// NewRouter creates a new smart router
func NewRouter() *Router {
	// Create a new random source with current time as seed
	src := rand.NewSource(time.Now().UnixNano())
	return &Router{
		rand: rand.New(src),
	}
}

// RouteModel applies smart routing logic to the requested model
// Returns the target model and which upstream to use
//
// Routing logic for "cursor-pro-sonnet" (10 parts total):
//   - 1/10 (10%) → "claude-sonnet-4-5" (via Antigravity)
//   - 2/10 (20%) → "claude-sonnet-4-5" (via GHCP)
//   - 4/10 (40%) → "gemini-3-flash" (via Antigravity)
//   - 3/10 (30%) → "gpt-5-mini" (via GHCP)
//
// Other routing:
//   - "claude-*" models → route to GHCP
//   - Other models → route to Antigravity
func (r *Router) RouteModel(requestedModel string) RoutingResult {
	// Smart routing for cursor-pro-sonnet with 4-way distribution
	if requestedModel == "cursor-pro-sonnet" {
		// Generate random number between 0.0 and 1.0
		roll := r.rand.Float64()

		// Distribution:
		// 0.00 - 0.10 (10%) = Sonnet via Antigravity
		// 0.10 - 0.30 (20%) = Sonnet via GHCP
		// 0.30 - 0.70 (40%) = Gemini via Antigravity
		// 0.70 - 1.00 (30%) = GPT via GHCP

		if roll < 0.10 {
			// 10%: Claude Sonnet via Antigravity
			return RoutingResult{
				Model:    "claude-sonnet-4-5-thinking",
				Upstream: UpstreamAntigravity,
			}
		} else if roll < 0.30 {
			// 20%: Claude Sonnet via GHCP
			return RoutingResult{
				Model:    "claude-sonnet-4.5",
				Upstream: UpstreamGHCP,
			}
		} else if roll < 0.70 {
			// 40%: Gemini via Antigravity
			return RoutingResult{
				Model:    "gemini-3-flash",
				Upstream: UpstreamAntigravity,
			}
		} else {
			// 30%: GPT via GHCP
			return RoutingResult{
				Model:    "gpt-5-mini",
				Upstream: UpstreamGHCP,
			}
		}
	}

	// Route Claude models to GHCP
	if len(requestedModel) >= 7 && requestedModel[:7] == "claude-" {
		return RoutingResult{
			Model:    requestedModel,
			Upstream: UpstreamGHCP,
		}
	}

	// All other models (including direct gpt-5-mini, gemini, etc) go to Antigravity
	return RoutingResult{
		Model:    requestedModel,
		Upstream: UpstreamAntigravity,
	}
}
