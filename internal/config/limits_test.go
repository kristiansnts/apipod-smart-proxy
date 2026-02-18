package config

import (
	"testing"
	"time"
)

func TestGetModelTimeouts(t *testing.T) {
	tests := []struct {
		model             string
		expectedTimeout   time.Duration
		expectedRetries   int
		expectedDelay     time.Duration
	}{
		{
			model:             "deepseek-chat",
			expectedTimeout:   10 * time.Minute,
			expectedRetries:   3,
			expectedDelay:     30 * time.Second,
		},
		{
			model:             "claude-3-haiku",
			expectedTimeout:   5 * time.Minute,
			expectedRetries:   2,
			expectedDelay:     10 * time.Second,
		},
		{
			model:             "gpt-4",
			expectedTimeout:   3 * time.Minute,
			expectedRetries:   2,
			expectedDelay:     5 * time.Second,
		},
		{
			model:             "gemini-pro",
			expectedTimeout:   4 * time.Minute,
			expectedRetries:   2,
			expectedDelay:     10 * time.Second,
		},
		{
			model:             "unknown-model",
			expectedTimeout:   5 * time.Minute,
			expectedRetries:   2,
			expectedDelay:     10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			timeouts := GetModelTimeouts(tt.model)
			if timeouts.RequestTimeout != tt.expectedTimeout {
				t.Errorf("RequestTimeout = %v, want %v", timeouts.RequestTimeout, tt.expectedTimeout)
			}
			if timeouts.MaxRetries != tt.expectedRetries {
				t.Errorf("MaxRetries = %d, want %d", timeouts.MaxRetries, tt.expectedRetries)
			}
			if timeouts.RetryDelay != tt.expectedDelay {
				t.Errorf("RetryDelay = %v, want %v", timeouts.RetryDelay, tt.expectedDelay)
			}
		})
	}
}

func TestIsSlowModel(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"deepseek-chat", true},
		{"deepseek-reasoner", true},
		{"gpt-3.5-turbo", true},
		{"model-free", true},
		{"claude-3-haiku", false},
		{"gpt-4", false},
		{"gemini-pro", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := IsSlowModel(tt.model)
			if result != tt.expected {
				t.Errorf("IsSlowModel(%s) = %v, want %v", tt.model, result, tt.expected)
			}
		})
	}
}