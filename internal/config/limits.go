package config

// ModelLimits defines token limits for different models to prevent bloated requests
type ModelLimits struct {
	MaxInputTokens  int
	MaxOutputTokens int
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