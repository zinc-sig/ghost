package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client represents a webhook HTTP client
type Client struct {
	httpClient  *http.Client
	config      *Config
	retryConfig *RetryConfig
	verbose     bool
}

// NewClient creates a new webhook client
func NewClient(config *Config, retryConfig *RetryConfig, verbose bool) *Client {
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if retryConfig == nil {
		retryConfig = DefaultRetryConfig()
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Per-request timeout
		},
		config:      config,
		retryConfig: retryConfig,
		verbose:     verbose,
	}
}

// Send sends the payload to the webhook with retry logic
func (c *Client) Send(ctx context.Context, payload interface{}) error {
	// Marshal the payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Create context with overall timeout
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	var lastErr error

	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		// Add backoff delay (skip on first attempt)
		if attempt > 0 {
			delay := calculateBackoff(attempt, c.retryConfig)

			if c.verbose {
				fmt.Fprintf(os.Stderr, "[WEBHOOK] Retry %d/%d after %v\n",
					attempt, c.retryConfig.MaxRetries, delay)
			}

			select {
			case <-time.After(delay):
				// Continue after delay
			case <-ctx.Done():
				return fmt.Errorf("webhook timeout after %d attempts: %w", attempt, ctx.Err())
			}
		}

		// Attempt to send
		statusCode, err := c.sendRequest(ctx, jsonPayload)

		if err == nil && statusCode >= 200 && statusCode < 300 {
			// Success!
			if c.verbose {
				fmt.Fprintf(os.Stderr, "[WEBHOOK] Successfully sent (status: %d)\n", statusCode)
			}
			return nil
		}

		// Record the error
		if err != nil {
			lastErr = fmt.Errorf("attempt %d failed: %w", attempt+1, err)
		} else {
			lastErr = fmt.Errorf("attempt %d failed with status %d", attempt+1, statusCode)
		}

		// Check if we should retry this status code
		if statusCode > 0 && !isRetryableStatus(statusCode) {
			if c.verbose {
				fmt.Fprintf(os.Stderr, "[WEBHOOK] Non-retryable status %d, giving up\n", statusCode)
			}
			return lastErr
		}
	}

	return fmt.Errorf("webhook failed after %d attempts: %w", c.retryConfig.MaxRetries+1, lastErr)
}

func (c *Client) sendRequest(ctx context.Context, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, c.config.Method, c.config.URL, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.config.Headers {
		req.Header.Set(k, v)
	}

	// Set authentication
	switch c.config.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.config.AuthToken)
	case "api-key":
		req.Header.Set("X-API-Key", c.config.AuthToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Drain response body to reuse connection
	io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}
