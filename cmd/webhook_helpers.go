package cmd

import (
	"fmt"
	"time"

	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/webhook"
)

// BuildWebhookConfig builds webhook configuration from all sources
func BuildWebhookConfig(config *WebhookConfig) (map[string]any, error) {
	// Convert WebhookConfig to strings for the generic builder
	// Note: The WebhookConfig has some non-string fields that need special handling
	
	// First, get the configuration from all sources
	result, err := contextparser.BuildContextWithPrefix(
		"GHOST_WEBHOOK",
		"", // No JSON string input for webhook yet
		[]string{}, // No KV pairs for webhook yet
		"", // No file input for webhook yet
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build webhook config: %w", err)
	}

	// If no environment config, create empty map
	if result == nil {
		result = make(map[string]any)
	}

	webhookConf, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("webhook config must be an object/map")
	}

	// Override with explicit flag values if set
	if config.URL != "" {
		webhookConf["url"] = config.URL
	}
	if config.AuthType != "" && config.AuthType != "none" {
		webhookConf["auth_type"] = config.AuthType
	}
	if config.AuthToken != "" {
		webhookConf["auth_token"] = config.AuthToken
	}
	if config.Timeout != "" && config.Timeout != "30s" {
		webhookConf["timeout"] = config.Timeout
	}
	if config.Retries != 3 {
		webhookConf["retries"] = config.Retries
	}
	if config.RetryDelay != "" && config.RetryDelay != "1s" {
		webhookConf["retry_delay"] = config.RetryDelay
	}

	return webhookConf, nil
}

// ParseWebhookConfigToInternal converts WebhookConfig to internal webhook structures
func ParseWebhookConfigToInternal(config *WebhookConfig) (*webhook.Config, *webhook.RetryConfig, error) {
	if config.URL == "" {
		return nil, nil, nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if config.Timeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	} else {
		webhookTimeoutDur = 30 * time.Second
	}

	// Parse retry delay
	var retryDelay time.Duration
	if config.RetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(config.RetryDelay)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	webhookConfig := &webhook.Config{
		URL:       config.URL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  config.AuthType,
		AuthToken: config.AuthToken,
	}

	retryConfig := &webhook.RetryConfig{
		MaxRetries:   config.Retries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return webhookConfig, retryConfig, nil
}

// MergeWebhookConfigFromEnv merges environment variables into WebhookConfig
func MergeWebhookConfigFromEnv(config *WebhookConfig) error {
	// Get environment configuration
	envConfig, err := BuildWebhookConfig(config)
	if err != nil {
		return err
	}

	// Apply environment values if flags are not set
	if config.URL == "" {
		if url, ok := envConfig["url"].(string); ok {
			config.URL = url
		}
	}
	if config.AuthType == "none" || config.AuthType == "" {
		if authType, ok := envConfig["auth_type"].(string); ok {
			config.AuthType = authType
		}
	}
	if config.AuthToken == "" {
		if authToken, ok := envConfig["auth_token"].(string); ok {
			config.AuthToken = authToken
		}
	}
	if config.Timeout == "30s" || config.Timeout == "" {
		if timeout, ok := envConfig["timeout"].(string); ok {
			config.Timeout = timeout
		}
	}
	if config.Retries == 3 {
		if retries, ok := envConfig["retries"].(int); ok {
			config.Retries = retries
		}
	}
	if config.RetryDelay == "1s" || config.RetryDelay == "" {
		if retryDelay, ok := envConfig["retry_delay"].(string); ok {
			config.RetryDelay = retryDelay
		}
	}

	return nil
}