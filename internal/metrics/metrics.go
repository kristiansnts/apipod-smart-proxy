package metrics

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

const maxLatencySamples = 10_000

// Metrics collects in-memory performance statistics.
type Metrics struct {
	total     int64
	success   int64
	cacheHits int64

	mu        sync.Mutex
	latencies []int64 // end-to-end ms, non-cache-hit requests only
}

// Snapshot is the computed performance snapshot returned by the /metrics endpoint.
type Snapshot struct {
	TotalRequests int64   `json:"total_requests"`
	SuccessRate   float64 `json:"success_rate"`   // percentage 0–100
	CacheHitRate  float64 `json:"cache_hit_rate"` // percentage 0–100
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P95LatencyMs  int64   `json:"p95_latency_ms"`
}

func New() *Metrics {
	return &Metrics{latencies: make([]int64, 0, 1024)}
}

// Record captures a single completed request.
//
// latencyMs – end-to-end handler duration.
// success   – response status was 2xx.
// cacheHit  – provider served a prompt-cache hit (cache_read_input_tokens > 0 for
//
//	Anthropic; prompt_tokens_details.cached_tokens > 0 for OpenAI).
//
// Cache-hit latencies are excluded from avg/P95 because they are artificially
// faster and would skew the numbers.
func (m *Metrics) Record(latencyMs int64, success, cacheHit bool) {
	atomic.AddInt64(&m.total, 1)
	if success {
		atomic.AddInt64(&m.success, 1)
	}
	if cacheHit {
		atomic.AddInt64(&m.cacheHits, 1)
		return
	}
	m.mu.Lock()
	if len(m.latencies) < maxLatencySamples {
		m.latencies = append(m.latencies, latencyMs)
	} else {
		// Rolling window: drop oldest sample.
		copy(m.latencies, m.latencies[1:])
		m.latencies[maxLatencySamples-1] = latencyMs
	}
	m.mu.Unlock()
}

// Snapshot computes and returns the current performance snapshot.
func (m *Metrics) Snapshot() Snapshot {
	total := atomic.LoadInt64(&m.total)
	success := atomic.LoadInt64(&m.success)
	cacheHits := atomic.LoadInt64(&m.cacheHits)

	var successRate, cacheHitRate float64
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
		cacheHitRate = float64(cacheHits) / float64(total) * 100
	}

	m.mu.Lock()
	lats := make([]int64, len(m.latencies))
	copy(lats, m.latencies)
	m.mu.Unlock()

	var avgMs float64
	var p95Ms int64
	if len(lats) > 0 {
		sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
		var sum int64
		for _, v := range lats {
			sum += v
		}
		avgMs = float64(sum) / float64(len(lats))
		idx := int(math.Ceil(float64(len(lats))*0.95)) - 1
		if idx < 0 {
			idx = 0
		}
		p95Ms = lats[idx]
	}

	return Snapshot{
		TotalRequests: total,
		SuccessRate:   math.Round(successRate*10) / 10,
		CacheHitRate:  math.Round(cacheHitRate*10) / 10,
		AvgLatencyMs:  math.Round(avgMs),
		P95LatencyMs:  p95Ms,
	}
}

// Handler returns an http.HandlerFunc that serves the performance snapshot as JSON.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := m.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	}
}
