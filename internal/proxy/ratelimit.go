package proxy

import (
	"sync"
	"time"
)

// RateLimiter manages in-memory RPM counters using a fixed window
type RateLimiter struct {
	mu      sync.Mutex
	counts  map[int64]int
	windows map[int64]int64
}

// NewRateLimiter creates a new in-memory rate limiter
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		counts:  make(map[int64]int),
		windows: make(map[int64]int64),
	}
}

// Allow checks if a quota item has reached its RPM limit and increments the counter
func (l *RateLimiter) Allow(quotaItemID int64, limit int) bool {
	if limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Unix() / 60 // Current minute window
	
	// Reset if we moved to a new minute
	if l.windows[quotaItemID] != now {
		l.windows[quotaItemID] = now
		l.counts[quotaItemID] = 0
	}

	if l.counts[quotaItemID] >= limit {
		return false
	}

	l.counts[quotaItemID]++
	return true
}
