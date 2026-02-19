package deviceauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// DeviceRequest represents a pending device authorization request
type DeviceRequest struct {
	DeviceCode string
	UserCode   string
	Status     string // "pending", "authorized", "expired"
	APIToken   string
	Username   string
	Plan       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// Store holds pending device authorization requests in memory
type Store struct {
	mu       sync.RWMutex
	requests map[string]*DeviceRequest // keyed by device_code
	byUser   map[string]string         // user_code -> device_code
}

// NewStore creates a new device auth store and starts cleanup goroutine
func NewStore() *Store {
	s := &Store{
		requests: make(map[string]*DeviceRequest),
		byUser:   make(map[string]string),
	}
	go s.cleanup()
	return s
}

// CreateRequest generates a new device code and user code
func (s *Store) CreateRequest(ttl time.Duration) *DeviceRequest {
	deviceCode := generateDeviceCode()
	userCode := generateUserCode()

	req := &DeviceRequest{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		Status:     "pending",
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(ttl),
	}

	s.mu.Lock()
	s.requests[deviceCode] = req
	s.byUser[userCode] = deviceCode
	s.mu.Unlock()

	return req
}

// GetByDeviceCode retrieves a request by device code
func (s *Store) GetByDeviceCode(deviceCode string) *DeviceRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	req, ok := s.requests[deviceCode]
	if !ok {
		return nil
	}
	if time.Now().After(req.ExpiresAt) {
		req.Status = "expired"
	}
	return req
}

// Authorize approves a device request by user code
func (s *Store) Authorize(userCode, apiToken, username, plan string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalizedCode := strings.ToUpper(strings.ReplaceAll(userCode, " ", ""))

	deviceCode, ok := s.byUser[normalizedCode]
	if !ok {
		return fmt.Errorf("invalid user code")
	}

	req, ok := s.requests[deviceCode]
	if !ok {
		return fmt.Errorf("device request not found")
	}

	if time.Now().After(req.ExpiresAt) {
		req.Status = "expired"
		return fmt.Errorf("device code expired")
	}

	req.Status = "authorized"
	req.APIToken = apiToken
	req.Username = username
	req.Plan = plan

	return nil
}

// cleanup periodically removes expired requests
func (s *Store) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		for code, req := range s.requests {
			if time.Now().After(req.ExpiresAt.Add(1 * time.Minute)) {
				delete(s.byUser, req.UserCode)
				delete(s.requests, code)
			}
		}
		s.mu.Unlock()
	}
}

func generateDeviceCode() string {
	b := make([]byte, 20)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateUserCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I, O, 0, 1
	part1 := randomString(chars, 4)
	part2 := randomString(chars, 4)
	return part1 + "-" + part2
}

func randomString(charset string, length int) string {
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[n.Int64()]
	}
	return string(result)
}
