package webhook

import (
	"testing"
	"time"
)

func TestCalculateBackoff(t *testing.T) {
	config := &RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}

	tests := []struct {
		name        string
		attempt     int
		minExpected time.Duration
		maxExpected time.Duration
	}{
		{
			name:        "no backoff for attempt 0",
			attempt:     0,
			minExpected: 0,
			maxExpected: 0,
		},
		{
			name:        "first retry",
			attempt:     1,
			minExpected: 90 * time.Millisecond,  // 100ms - 10% jitter
			maxExpected: 110 * time.Millisecond, // 100ms + 10% jitter
		},
		{
			name:        "second retry",
			attempt:     2,
			minExpected: 180 * time.Millisecond, // 200ms - 10% jitter
			maxExpected: 220 * time.Millisecond, // 200ms + 10% jitter
		},
		{
			name:        "third retry",
			attempt:     3,
			minExpected: 360 * time.Millisecond, // 400ms - 10% jitter
			maxExpected: 440 * time.Millisecond, // 400ms + 10% jitter
		},
		{
			name:        "capped at max delay",
			attempt:     10,
			minExpected: 4500 * time.Millisecond, // 5s - 10% jitter
			maxExpected: 5500 * time.Millisecond, // 5s + 10% jitter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := calculateBackoff(tt.attempt, config)

			if tt.minExpected == 0 && tt.maxExpected == 0 {
				if delay != 0 {
					t.Errorf("Expected no delay for attempt %d, got %v", tt.attempt, delay)
				}
			} else {
				if delay < tt.minExpected || delay > tt.maxExpected {
					t.Errorf("Expected delay between %v and %v for attempt %d, got %v",
						tt.minExpected, tt.maxExpected, tt.attempt, delay)
				}
			}
		})
	}
}

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{200, false}, // OK
		{201, false}, // Created
		{204, false}, // No Content
		{400, false}, // Bad Request
		{401, false}, // Unauthorized
		{403, false}, // Forbidden
		{404, false}, // Not Found
		{408, true},  // Request Timeout
		{429, true},  // Too Many Requests
		{500, true},  // Internal Server Error
		{501, false}, // Not Implemented
		{502, true},  // Bad Gateway
		{503, true},  // Service Unavailable
		{504, true},  // Gateway Timeout
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.code)), func(t *testing.T) {
			result := isRetryableStatus(tt.code)
			if result != tt.expected {
				t.Errorf("isRetryableStatus(%d) = %v; want %v", tt.code, result, tt.expected)
			}
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries to be 3, got %d", config.MaxRetries)
	}

	if config.InitialDelay != 1*time.Second {
		t.Errorf("Expected InitialDelay to be 1s, got %v", config.InitialDelay)
	}

	if config.MaxDelay != 30*time.Second {
		t.Errorf("Expected MaxDelay to be 30s, got %v", config.MaxDelay)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier to be 2.0, got %f", config.Multiplier)
	}
}

func BenchmarkCalculateBackoff(b *testing.B) {
	config := DefaultRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateBackoff(3, config)
	}
}
