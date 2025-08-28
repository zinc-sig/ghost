package webhook

import "time"

// Config holds webhook endpoint configuration
type Config struct {
	URL       string            // Webhook endpoint URL
	Method    string            // HTTP method (default: POST)
	Headers   map[string]string // Custom headers
	Timeout   time.Duration     // Overall timeout for all retries
	AuthType  string            // Authentication type: none, bearer, api-key
	AuthToken string            // Authentication token
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries   int           // Maximum retry attempts (default: 3)
	InitialDelay time.Duration // Initial delay between retries (default: 1s)
	MaxDelay     time.Duration // Maximum delay (default: 30s)
	Multiplier   float64       // Backoff multiplier (default: 2.0)
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}
