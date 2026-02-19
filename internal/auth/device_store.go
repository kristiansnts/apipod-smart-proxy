package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

type DeviceCode struct {
	DeviceCode string
	UserCode   string
	ExpiresAt  time.Time
	Interval   int
	Authorized bool
	UserID     int
	APIToken   string
	Username   string
	Plan       string
}

type DeviceStore struct {
	mu    sync.RWMutex
	codes map[string]*DeviceCode // keyed by device_code
	byUser map[string]*DeviceCode // keyed by user_code (uppercase)
}

func NewDeviceStore() *DeviceStore {
	ds := &DeviceStore{
		codes:  make(map[string]*DeviceCode),
		byUser: make(map[string]*DeviceCode),
	}
	go ds.cleanupLoop()
	return ds
}

func (ds *DeviceStore) CreateCode() (*DeviceCode, error) {
	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, err
	}

	userCode, err := generateUserCode()
	if err != nil {
		return nil, err
	}

	dc := &DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ExpiresAt:  time.Now().Add(10 * time.Minute),
		Interval:   5,
	}

	ds.mu.Lock()
	ds.codes[deviceCode] = dc
	ds.byUser[strings.ToUpper(userCode)] = dc
	ds.mu.Unlock()

	return dc, nil
}

func (ds *DeviceStore) GetByDeviceCode(deviceCode string) *DeviceCode {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	dc, ok := ds.codes[deviceCode]
	if !ok {
		return nil
	}
	if time.Now().After(dc.ExpiresAt) {
		return nil
	}
	return dc
}

func (ds *DeviceStore) GetByUserCode(userCode string) *DeviceCode {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	dc, ok := ds.byUser[strings.ToUpper(userCode)]
	if !ok {
		return nil
	}
	if time.Now().After(dc.ExpiresAt) {
		return nil
	}
	return dc
}

func (ds *DeviceStore) Authorize(userCode string, userID int, apiToken, username, plan string) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	dc, ok := ds.byUser[strings.ToUpper(userCode)]
	if !ok || time.Now().After(dc.ExpiresAt) {
		return false
	}

	dc.Authorized = true
	dc.UserID = userID
	dc.APIToken = apiToken
	dc.Username = username
	dc.Plan = plan
	return true
}

func (ds *DeviceStore) Remove(deviceCode string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if dc, ok := ds.codes[deviceCode]; ok {
		delete(ds.byUser, strings.ToUpper(dc.UserCode))
		delete(ds.codes, deviceCode)
	}
}

func (ds *DeviceStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ds.mu.Lock()
		now := time.Now()
		for key, dc := range ds.codes {
			if now.After(dc.ExpiresAt) {
				delete(ds.byUser, strings.ToUpper(dc.UserCode))
				delete(ds.codes, key)
			}
		}
		ds.mu.Unlock()
	}
}

func generateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateUserCode() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I, O, 0, 1
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	code := make([]byte, 8)
	for i := range code {
		code[i] = charset[int(b[i])%len(charset)]
	}
	return fmt.Sprintf("%s-%s", string(code[:4]), string(code[4:])), nil
}
