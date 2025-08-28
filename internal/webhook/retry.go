package webhook

import (
	"math"
	"math/rand"
	"time"
)

// calculateBackoff calculates the backoff duration for a given retry attempt
func calculateBackoff(attempt int, config *RetryConfig) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Exponential: delay = initialDelay * (multiplier ^ (attempt-1))
	delay := float64(config.InitialDelay) * math.Pow(config.Multiplier, float64(attempt-1))

	// Cap at maximum
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}

	// Add small jitter (±10%) to prevent thundering herd
	jitter := delay * 0.1
	delay = delay + (rand.Float64()*2-1)*jitter

	return time.Duration(delay)
}

// isRetryableStatus checks if an HTTP status code should trigger a retry
func isRetryableStatus(code int) bool {
	switch code {
	case 408, // Request Timeout
		429, // Too Many Requests
		500, // Internal Server Error
		502, // Bad Gateway
		503, // Service Unavailable
		504: // Gateway Timeout
		return true
	default:
		return false
	}
}
