package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// RuntimeConfig is what the proxy needs to handle any request.
// Comes from either static file (OSS) or remote API (SaaS).
type RuntimeConfig struct {
	Allowed       bool     `json:"allowed"`
	Mode          string   `json:"mode"`           // "byok" | "platform"
	OrgID         uint     `json:"org_id"`
	APIKeyID      uint     `json:"api_key_id"`
	SubID         int64    `json:"sub_id"`
	RateLimitRPM  int      `json:"rate_limit_rpm"`
	DailyQuota    int      `json:"daily_quota"`
	DailyUsed     int      `json:"daily_used"`
	AllowedModels []string `json:"allowed_models"`
	Priority      string   `json:"priority"`
	TokenBalance  *int64   `json:"token_balance"`
	Reason        string   `json:"reason"`

	// For BYOK: upstream keys per provider
	UpstreamKeys []UpstreamKey `json:"upstream_keys"`

	// For BYOK: user's selected active model
	ActiveModel *ActiveModelConfig `json:"active_model"`
}

// ActiveModelConfig is the user's selected model for BYOK mode.
type ActiveModelConfig struct {
	ModelName    string `json:"model_name"`
	ProviderID   uint   `json:"provider_id"`
	ProviderType string `json:"provider_type"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
}

// UpstreamKey is a user-provided key for a specific provider.
type UpstreamKey struct {
	ProviderID   uint   `json:"provider_id"`
	ProviderType string `json:"provider_type"`
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
}

// ConfigLoader provides runtime config for a given API key.
type ConfigLoader interface {
	GetRuntimeConfig(apiKey string) (*RuntimeConfig, error)
}

// --- Remote Config Loader (SaaS mode) ---

// RemoteConfigLoader calls the Laravel backend for runtime config.
type RemoteConfigLoader struct {
	baseURL    string
	secret     string
	httpClient *http.Client

	// Cache: last known config per key (fallback if API is down)
	cache   map[string]*cachedConfig
	cacheMu sync.RWMutex
}

type cachedConfig struct {
	config    *RuntimeConfig
	fetchedAt time.Time
}

const cacheTTL = 5 * time.Minute

func NewRemoteConfigLoader(baseURL, secret string) *RemoteConfigLoader {
	return &RemoteConfigLoader{
		baseURL: baseURL,
		secret:  secret,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		cache: make(map[string]*cachedConfig),
	}
}

func (r *RemoteConfigLoader) GetRuntimeConfig(apiKey string) (*RuntimeConfig, error) {
	req, err := http.NewRequest("GET", r.baseURL+"/api/internal/runtime-config?api_key="+apiKey, nil)
	if err != nil {
		return r.fallbackCache(apiKey, err)
	}
	req.Header.Set("X-Internal-Secret", r.secret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return r.fallbackCache(apiKey, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return r.fallbackCache(apiKey, err)
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return r.fallbackCache(apiKey, err)
	}

	// Update cache
	r.cacheMu.Lock()
	r.cache[apiKey] = &cachedConfig{config: &cfg, fetchedAt: time.Now()}
	r.cacheMu.Unlock()

	return &cfg, nil
}

// fallbackCache returns cached config if available and younger than cacheTTL.
func (r *RemoteConfigLoader) fallbackCache(apiKey string, originalErr error) (*RuntimeConfig, error) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	if cached, ok := r.cache[apiKey]; ok {
		if time.Since(cached.fetchedAt) < cacheTTL {
			return cached.config, nil
		}
	}

	return nil, fmt.Errorf("runtime config API unreachable and no valid cache: %w", originalErr)
}

// --- Static Config Loader (OSS / self-host mode) ---

// StaticConfigLoader reads config from a JSON file for self-hosted users.
type StaticConfigLoader struct {
	configs map[string]*RuntimeConfig // apiKey → config
}

// StaticConfigFile is the format of the config.json file.
type StaticConfigFile struct {
	Keys []StaticKeyConfig `json:"keys"`
}

type StaticKeyConfig struct {
	APIKey        string     `json:"api_key"`
	Mode          string     `json:"mode"`
	SubID         int64      `json:"sub_id"`
	RateLimitRPM  int        `json:"rate_limit_rpm"`
	DailyQuota    int        `json:"daily_quota"`
	AllowedModels []string   `json:"allowed_models"`
	UpstreamKeys  []UpstreamKey `json:"upstream_keys"`
}

func NewStaticConfigLoader(filePath string) (*StaticConfigLoader, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var file StaticConfigFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	configs := make(map[string]*RuntimeConfig)
	for _, key := range file.Keys {
		configs[key.APIKey] = &RuntimeConfig{
			Allowed:       true,
			Mode:          key.Mode,
			SubID:         key.SubID,
			RateLimitRPM:  key.RateLimitRPM,
			DailyQuota:    key.DailyQuota,
			AllowedModels: key.AllowedModels,
			Priority:      "normal",
			UpstreamKeys:  key.UpstreamKeys,
		}
	}

	return &StaticConfigLoader{configs: configs}, nil
}

func (s *StaticConfigLoader) GetRuntimeConfig(apiKey string) (*RuntimeConfig, error) {
	cfg, ok := s.configs[apiKey]
	if !ok {
		return &RuntimeConfig{Allowed: false, Reason: "Invalid API key"}, nil
	}
	return cfg, nil
}
