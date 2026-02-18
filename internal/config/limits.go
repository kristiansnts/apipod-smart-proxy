package config

import "time"

// ModelLimits defines token limits for different models to prevent bloated requests
type ModelLimits struct {
	MaxInputTokens  int
	MaxOutputTokens int
}

// ModelTimeouts defines timeout configurations for different model tiers
type ModelTimeouts struct {
	RequestTimeout       time.Duration // HTTP request timeout
	ToolContinueTimeout  time.Duration // Timeout for tool continuation requests
	MaxRetries          int           // Maximum retry attempts
	RetryDelay          time.Duration // Base delay between retries
}

// GetModelLimits returns token limits for a given model
func GetModelLimits(model string) ModelLimits {
	switch {
	// DeepSeek models - aggressive limits to prevent token bloat
	case contains(model, "deepseek"):
		return ModelLimits{
			MaxInputTokens:  8000,  // Reduced from default to prevent bloat
			MaxOutputTokens: 4096,
		}
	
	// Claude models - standard limits
	case contains(model, "claude"):
		return ModelLimits{
			MaxInputTokens:  100000, // Claude can handle larger contexts
			MaxOutputTokens: 8192,
		}
	
	// GPT models - standard limits
	case contains(model, "gpt"):
		return ModelLimits{
			MaxInputTokens:  16000,
			MaxOutputTokens: 4096,
		}
	
	// Gemini models
	case contains(model, "gemini"):
		return ModelLimits{
			MaxInputTokens:  30000,
			MaxOutputTokens: 8192,
		}
	
	// Default conservative limits
	default:
		return ModelLimits{
			MaxInputTokens:  8000,
			MaxOutputTokens: 2048,
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		   len(s) > len(substr) && findInString(s, substr)
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// GetModelTimeouts returns timeout configurations for a given model
func GetModelTimeouts(model string) ModelTimeouts {
	switch {
	// Free/slow models - much longer timeouts and more retries
	case contains(model, "deepseek"):
		return ModelTimeouts{
			RequestTimeout:      10 * time.Minute, // Very long for slow models
			ToolContinueTimeout: 15 * time.Minute, // Extra time for tool continuations
			MaxRetries:         3,
			RetryDelay:         30 * time.Second,
		}
	
	// Claude models - standard timeouts
	case contains(model, "claude"):
		return ModelTimeouts{
			RequestTimeout:      5 * time.Minute,
			ToolContinueTimeout: 8 * time.Minute,
			MaxRetries:         2,
			RetryDelay:         10 * time.Second,
		}
	
	// GPT models - faster response times
	case contains(model, "gpt"):
		return ModelTimeouts{
			RequestTimeout:      3 * time.Minute,
			ToolContinueTimeout: 5 * time.Minute,
			MaxRetries:         2,
			RetryDelay:         5 * time.Second,
		}
	
	// Gemini models
	case contains(model, "gemini"):
		return ModelTimeouts{
			RequestTimeout:      4 * time.Minute,
			ToolContinueTimeout: 6 * time.Minute,
			MaxRetries:         2,
			RetryDelay:         10 * time.Second,
		}
	
	// Default conservative settings for unknown models
	default:
		return ModelTimeouts{
			RequestTimeout:      5 * time.Minute,
			ToolContinueTimeout: 8 * time.Minute,
			MaxRetries:         2,
			RetryDelay:         10 * time.Second,
		}
	}
}

// IsSlowModel returns true if the model is considered slow/free tier
func IsSlowModel(model string) bool {
	return contains(model, "deepseek") || 
		   contains(model, "free") ||
		   contains(model, "3.5-turbo") // GPT-3.5 is often rate-limited
}