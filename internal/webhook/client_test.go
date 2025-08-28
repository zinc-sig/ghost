package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
)

func TestNewClient(t *testing.T) {
	config := &Config{
		URL:       "https://example.com/webhook",
		AuthType:  "bearer",
		AuthToken: "test-token",
	}

	client := NewClient(config, nil, false)

	if client.config.Method != "POST" {
		t.Errorf("Expected default method to be POST, got %s", client.config.Method)
	}

	if client.config.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout to be 30s, got %v", client.config.Timeout)
	}

	if client.retryConfig.MaxRetries != 3 {
		t.Errorf("Expected default max retries to be 3, got %d", client.retryConfig.MaxRetries)
	}
}

func TestClientSend_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
		}

		// Verify JSON payload
		var payload output.Result
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("Failed to unmarshal payload: %v", err)
		}

		if payload.Command != "test command" {
			t.Errorf("Expected command 'test command', got %s", payload.Command)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Method:  "POST",
		Timeout: 5 * time.Second,
	}

	client := NewClient(config, DefaultRetryConfig(), false)

	payload := &output.Result{
		Command:       "test command",
		Status:        "success",
		Input:         "input.txt",
		Output:        "output.txt",
		Stderr:        "stderr.txt",
		ExitCode:      0,
		ExecutionTime: 100,
	}

	ctx := context.Background()
	err := client.Send(ctx, payload)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestClientSend_AuthHeaders(t *testing.T) {
	tests := []struct {
		name           string
		authType       string
		authToken      string
		expectedHeader string
		expectedValue  string
	}{
		{
			name:           "bearer auth",
			authType:       "bearer",
			authToken:      "test-token",
			expectedHeader: "Authorization",
			expectedValue:  "Bearer test-token",
		},
		{
			name:           "api-key auth",
			authType:       "api-key",
			authToken:      "api-key-value",
			expectedHeader: "X-API-Key",
			expectedValue:  "api-key-value",
		},
		{
			name:           "no auth",
			authType:       "none",
			authToken:      "",
			expectedHeader: "",
			expectedValue:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.expectedHeader != "" {
					value := r.Header.Get(tt.expectedHeader)
					if value != tt.expectedValue {
						t.Errorf("Expected %s header to be '%s', got '%s'",
							tt.expectedHeader, tt.expectedValue, value)
					}
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			config := &Config{
				URL:       server.URL,
				AuthType:  tt.authType,
				AuthToken: tt.authToken,
				Timeout:   5 * time.Second,
			}

			client := NewClient(config, DefaultRetryConfig(), false)

			payload := &output.Result{Command: "test"}
			ctx := context.Background()
			if err := client.Send(ctx, payload); err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestClientSend_RetryOnFailure(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count <= 2 {
			// Fail first two attempts with retryable status
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			// Succeed on third attempt
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 10 * time.Second,
	}

	retryConfig := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	client := NewClient(config, retryConfig, false)

	payload := &output.Result{Command: "test"}
	ctx := context.Background()
	err := client.Send(ctx, payload)

	if err != nil {
		t.Errorf("Expected successful send after retries, got error: %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", finalAttempts)
	}
}

func TestClientSend_NonRetryableStatus(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		// Return non-retryable status
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	retryConfig := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
	}

	client := NewClient(config, retryConfig, false)

	payload := &output.Result{Command: "test"}
	ctx := context.Background()
	err := client.Send(ctx, payload)

	if err == nil {
		t.Error("Expected error for non-retryable status")
	}

	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("Expected error to contain status 400, got: %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 1 {
		t.Errorf("Expected 1 attempt (no retries for non-retryable status), got %d", finalAttempts)
	}
}

func TestClientSend_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 100 * time.Millisecond, // Very short timeout
	}

	client := NewClient(config, &RetryConfig{MaxRetries: 0}, false)

	payload := &output.Result{Command: "test"}
	ctx := context.Background()
	err := client.Send(ctx, payload)

	if err == nil {
		t.Error("Expected timeout error")
	}

	// The error should mention either "timeout" or "deadline exceeded"
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("Expected timeout/deadline error, got: %v", err)
	}
}

func TestClientSend_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First request returns retryable error
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	retryConfig := &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond, // Short delay
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
	}

	client := NewClient(config, retryConfig, false)

	payload := &output.Result{Command: "test"}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a very short delay (before first retry)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := client.Send(ctx, payload)

	if err == nil {
		t.Error("Expected context cancellation error")
	}

	// The error should mention either "context canceled" or be a timeout after attempts
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "timeout after") {
		t.Errorf("Expected context canceled or timeout error, got: %v", err)
	}
}

func TestClientSend_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("Expected X-Custom-Header to be 'custom-value', got '%s'",
				r.Header.Get("X-Custom-Header"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Headers: map[string]string{"X-Custom-Header": "custom-value"},
		Timeout: 5 * time.Second,
	}

	client := NewClient(config, nil, false)

	payload := &output.Result{Command: "test"}
	ctx := context.Background()
	if err := client.Send(ctx, payload); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestClientSend_MaxRetriesExceeded(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		// Always return retryable error
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	retryConfig := &RetryConfig{
		MaxRetries:   2,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	client := NewClient(config, retryConfig, false)

	payload := &output.Result{Command: "test"}
	ctx := context.Background()
	err := client.Send(ctx, payload)

	if err == nil {
		t.Error("Expected error after max retries")
	}

	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("Expected error message to mention attempts, got: %v", err)
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 { // Initial + 2 retries
		t.Errorf("Expected 3 attempts, got %d", finalAttempts)
	}
}
