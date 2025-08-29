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
	if cfg.URL != "" {
		webhookConf["url"] = cfg.URL
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

// ParseWebhookConfigToInternal converts WebhookConfig to internal webhook structures
func ParseWebhookConfigToInternal(cfg *config.WebhookConfig) (*webhook.Config, *webhook.RetryConfig, error) {
	if cfg.URL == "" {
		return nil, nil, nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if cfg.Timeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	} else {
		webhookTimeoutDur = 30 * time.Second
	}

	// Parse retry delay
	var retryDelay time.Duration
	if cfg.RetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(cfg.RetryDelay)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	webhookConfig := &webhook.Config{
		URL:       cfg.URL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  cfg.AuthType,
		AuthToken: cfg.AuthToken,
	}

	retryConfig := &webhook.RetryConfig{
		MaxRetries:   cfg.Retries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return webhookConfig, retryConfig, nil
}

// MergeWebhookConfigFromEnv merges environment variables into WebhookConfig
func MergeWebhookConfigFromEnv(cfg *config.WebhookConfig) error {
	// Get environment configuration
	envConfig, err := BuildWebhookConfig(cfg)
	if err != nil {
		return err
	}

	// Apply environment values if flags are not set
	if cfg.URL == "" {
		if url, ok := envConfig["url"].(string); ok {
			cfg.URL = url
		}
	}
	if cfg.AuthType == "none" || cfg.AuthType == "" {
		if authType, ok := envConfig["auth_type"].(string); ok {
			cfg.AuthType = authType
		}
	}
	if cfg.AuthToken == "" {
		if authToken, ok := envConfig["auth_token"].(string); ok {
			cfg.AuthToken = authToken
		}
	}
	if cfg.Timeout == "30s" || cfg.Timeout == "" {
		if timeout, ok := envConfig["timeout"].(string); ok {
			cfg.Timeout = timeout
		}
	}
	if cfg.Retries == 3 {
		if retries, ok := envConfig["retries"].(int); ok {
			cfg.Retries = retries
		}
	}
	if cfg.RetryDelay == "1s" || cfg.RetryDelay == "" {
		if retryDelay, ok := envConfig["retry_delay"].(string); ok {
			cfg.RetryDelay = retryDelay
		}
	}

	return nil
}