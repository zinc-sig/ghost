package helpers

import (
	"fmt"
	"time"

	"github.com/zinc-sig/ghost/cmd/config"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/webhook"
)

// BuildWebhookConfig builds webhook configuration from all sources
func BuildWebhookConfig(cfg *config.WebhookConfig) (map[string]any, error) {
	// Use the generic builder with all configuration sources
	// Precedence: env < file < json < kv < direct flags
	result, err := contextparser.BuildContextWithPrefix(
		"GHOST_WEBHOOK",
		cfg.Config,     // JSON string configuration
		cfg.ConfigKV,   // Key-value pairs
		cfg.ConfigFile, // Config file path
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build webhook config: %w", err)
	}

	// If no config from any source, create empty map
	if result == nil {
		result = make(map[string]any)
	}

	webhookConf, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("webhook config must be an object/map")
	}

	// Override with explicit flag values if set (highest precedence)
	if cfg.URL != "" {
		webhookConf["url"] = cfg.URL
	}
	if cfg.Method != "" && cfg.Method != "POST" {
		webhookConf["method"] = cfg.Method
	}
	if cfg.AuthType != "" && cfg.AuthType != "none" {
		webhookConf["auth_type"] = cfg.AuthType
	}
	if cfg.AuthToken != "" {
		webhookConf["auth_token"] = cfg.AuthToken
	}
	if cfg.Timeout != "" && cfg.Timeout != "30s" {
		webhookConf["timeout"] = cfg.Timeout
	}
	if cfg.Retries != 3 {
		webhookConf["retries"] = cfg.Retries
	}
	if cfg.RetryDelay != "" && cfg.RetryDelay != "1s" {
		webhookConf["retry_delay"] = cfg.RetryDelay
	}

	return webhookConf, nil
}

// ParseWebhookConfigToInternal converts built webhook config map to internal webhook structures
func ParseWebhookConfigToInternal(cfg *config.WebhookConfig) (*webhook.Config, *webhook.RetryConfig, error) {
	// Build the consolidated configuration from all sources
	configMap, err := BuildWebhookConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	
	// Check if webhook is configured
	url, _ := configMap["url"].(string)
	if url == "" {
		return nil, nil, nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration = 30 * time.Second
	if timeout, ok := configMap["timeout"].(string); ok && timeout != "" {
		webhookTimeoutDur, err = time.ParseDuration(timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	}

	// Parse retry delay
	var retryDelay time.Duration = 1 * time.Second
	if delay, ok := configMap["retry_delay"].(string); ok && delay != "" {
		retryDelay, err = time.ParseDuration(delay)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	}
	
	// Get HTTP method (default to POST)
	method, _ := configMap["method"].(string)
	if method == "" {
		method = "POST"
	}
	
	// Get auth settings
	authType, _ := configMap["auth_type"].(string)
	if authType == "" {
		authType = "none"
	}
	authToken, _ := configMap["auth_token"].(string)
	
	// Get retries (handle both int and float64 from JSON)
	maxRetries := 3
	if r, ok := configMap["retries"].(int); ok {
		maxRetries = r
	} else if r, ok := configMap["retries"].(float64); ok {
		maxRetries = int(r)
	}

	webhookConfig := &webhook.Config{
		URL:       url,
		Method:    method,
		Timeout:   webhookTimeoutDur,
		AuthType:  authType,
		AuthToken: authToken,
	}

	retryConfig := &webhook.RetryConfig{
		MaxRetries:   maxRetries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return webhookConfig, retryConfig, nil
}

