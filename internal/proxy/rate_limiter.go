package proxy

import (
	"sync"
	"time"
)

// RateLimiter enforces per-org RPM and daily request limits.
// In-memory only — no external dependencies.
type RateLimiter struct {
	buckets   map[uint]*orgBucket
	mu        sync.Mutex
}

type orgBucket struct {
	// RPM tracking: sliding window (1 minute)
	requestTimestamps []time.Time

	// Daily tracking
	dailyCount int
	dailyDate  string // "2006-01-02"
}

// NewRateLimiter creates a new in-memory rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[uint]*orgBucket),
	}
}

// AllowRequest checks if the org is within its RPM limit.
// Returns false if limit exceeded.
func (rl *RateLimiter) AllowRequest(orgID uint, rpm int) bool {
	if rpm <= 0 {
		return true // no limit
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket := rl.getOrCreateBucket(orgID)
	now := time.Now()
	windowStart := now.Add(-1 * time.Minute)

	// Prune old timestamps outside the 1-minute window
	valid := bucket.requestTimestamps[:0]
	for _, ts := range bucket.requestTimestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}
	bucket.requestTimestamps = valid

	// Check limit
	if len(bucket.requestTimestamps) >= rpm {
		return false
	}

	// Record this request
	bucket.requestTimestamps = append(bucket.requestTimestamps, now)
	return true
}

// CheckDailyQuota checks if the org is within its daily request quota.
// dailyUsed comes from the backend config, but we also track locally
// for requests not yet committed.
func (rl *RateLimiter) CheckDailyQuota(orgID uint, dailyUsed, dailyQuota int) bool {
	if dailyQuota <= 0 {
		return true // no cap
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket := rl.getOrCreateBucket(orgID)
	today := time.Now().Format("2006-01-02")

	// Reset if new day
	if bucket.dailyDate != today {
		bucket.dailyCount = dailyUsed // sync from backend
		bucket.dailyDate = today
	}

	if bucket.dailyCount >= dailyQuota {
		return false
	}

	bucket.dailyCount++
	return true
}

func (rl *RateLimiter) getOrCreateBucket(orgID uint) *orgBucket {
	if b, ok := rl.buckets[orgID]; ok {
		return b
	}
	b := &orgBucket{
		dailyDate: time.Now().Format("2006-01-02"),
	}
	rl.buckets[orgID] = b
	return b
}
