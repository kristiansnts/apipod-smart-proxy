package pool

import (
	"sync"
	"time"
)

// modelState tracks rate limit counters for a single LLM model.
type modelState struct {
	rpm *int // nil = unlimited
	tpm *int
	rpd *int

	minuteRequests int
	minuteTokens   int
	dayRequests    int
}

// ModelLimiter enforces per-model rate limits (RPM, TPM, RPD).
type ModelLimiter struct {
	mu     sync.Mutex
	models map[int64]*modelState // keyed by llm_model_id
}

// NewModelLimiter creates a limiter with background reset goroutines.
func NewModelLimiter() *ModelLimiter {
	ml := &ModelLimiter{
		models: make(map[int64]*modelState),
	}
	go ml.resetMinute()
	go ml.resetDay()
	return ml
}

func (ml *ModelLimiter) resetMinute() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		ml.mu.Lock()
		for _, s := range ml.models {
			s.minuteRequests = 0
			s.minuteTokens = 0
		}
		ml.mu.Unlock()
	}
}

func (ml *ModelLimiter) resetDay() {
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		ml.mu.Lock()
		for _, s := range ml.models {
			s.dayRequests = 0
		}
		ml.mu.Unlock()
	}
}

// getOrCreate returns the state for a model, creating it if needed.
func (ml *ModelLimiter) getOrCreate(modelID int64) *modelState {
	s, ok := ml.models[modelID]
	if !ok {
		s = &modelState{}
		ml.models[modelID] = s
	}
	return s
}

// SetLimits updates the rate limits for a model (called on each request with fresh DB values).
func (ml *ModelLimiter) SetLimits(modelID int64, rpm, tpm, rpd *int) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	s := ml.getOrCreate(modelID)
	s.rpm = rpm
	s.tpm = tpm
	s.rpd = rpd
}

// AllowRequest checks RPM and RPD limits. Returns true if the request is allowed.
// Increments counters on success.
func (ml *ModelLimiter) AllowRequest(modelID int64) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	s := ml.getOrCreate(modelID)

	if s.rpm != nil && s.minuteRequests >= *s.rpm {
		return false
	}
	if s.rpd != nil && s.dayRequests >= *s.rpd {
		return false
	}

	s.minuteRequests++
	s.dayRequests++
	return true
}

// CheckTPM returns true if the model's TPM limit has NOT been exceeded yet.
func (ml *ModelLimiter) CheckTPM(modelID int64) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	s := ml.getOrCreate(modelID)
	if s.tpm != nil && s.minuteTokens >= *s.tpm {
		return false
	}
	return true
}

// RecordTokens adds token usage for TPM tracking.
func (ml *ModelLimiter) RecordTokens(modelID int64, tokens int) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	s := ml.getOrCreate(modelID)
	s.minuteTokens += tokens
}
