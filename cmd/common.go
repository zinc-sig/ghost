package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/runner"
	"github.com/zinc-sig/ghost/internal/webhook"
)

// createJSONResult creates a JSON result from execution results
func createJSONResult(inputPath, outputPath, stderrPath string, result *runner.Result, timeoutMs int64, scoreSet bool, score int, context any) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Status:        string(result.Status),
		Input:         inputPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
		Context:       context,
	}

	// Add timeout if it was set
	if timeoutMs > 0 {
		jsonResult.Timeout = &timeoutMs
	}

	if scoreSet {
		if result.ExitCode == 0 {
			jsonResult.Score = &score
		} else {
			zero := 0
			jsonResult.Score = &zero
		}
	}

	return jsonResult
}

// createDiffJSONResult creates a JSON result for diff command with expected field
func createDiffJSONResult(inputPath, expectedPath, outputPath, stderrPath string, result *runner.Result, timeoutMs int64, scoreSet bool, score int, context any) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Status:        string(result.Status),
		Input:         inputPath,
		Expected:      &expectedPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
		Context:       context,
	}

	// Add timeout if it was set
	if timeoutMs > 0 {
		jsonResult.Timeout = &timeoutMs
	}

	if scoreSet {
		if result.ExitCode == 0 {
			jsonResult.Score = &score
		} else {
			zero := 0
			jsonResult.Score = &zero
		}
	}

	return jsonResult
}

// outputJSON marshals and prints the result as JSON
func outputJSON(result *output.Result) error {
	jsonOutput, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}

	fmt.Println(string(jsonOutput))
	return nil
}

// Global webhook configuration variables (internal use)
var (
	runWebhookConfigParsed  *webhook.Config
	runRetryConfig          *webhook.RetryConfig
	diffWebhookConfigParsed *webhook.Config
	diffRetryConfig         *webhook.RetryConfig
)

// parseWebhookConfig parses webhook configuration for run command
func parseWebhookConfig(config *WebhookConfig) error {
	if config.URL == "" {
		return nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if config.Timeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return fmt.Errorf("invalid webhook timeout duration: %w", err)
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
			return fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	runWebhookConfigParsed = &webhook.Config{
		URL:       config.URL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  config.AuthType,
		AuthToken: config.AuthToken,
	}

	runRetryConfig = &webhook.RetryConfig{
		MaxRetries:   config.Retries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return nil
}

// parseDiffWebhookConfig parses webhook configuration for diff command
func parseDiffWebhookConfig(config *WebhookConfig) error {
	if config.URL == "" {
		return nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if config.Timeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(config.Timeout)
		if err != nil {
			return fmt.Errorf("invalid webhook timeout duration: %w", err)
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
			return fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	diffWebhookConfigParsed = &webhook.Config{
		URL:       config.URL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  config.AuthType,
		AuthToken: config.AuthToken,
	}

	diffRetryConfig = &webhook.RetryConfig{
		MaxRetries:   config.Retries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return nil
}

// outputJSONAndWebhook outputs JSON to stdout and optionally sends to webhook
func outputJSONAndWebhook(result *output.Result, verbose bool) error {
	// Determine which webhook config to use based on command
	var config *webhook.Config
	var retryConfig *webhook.RetryConfig

	// Check if this is a diff command by looking for Expected field
	if result.Expected != nil {
		config = diffWebhookConfigParsed
		retryConfig = diffRetryConfig
	} else {
		config = runWebhookConfigParsed
		retryConfig = runRetryConfig
	}

	// Send webhook if configured (before outputting to stdout)
	if config != nil && config.URL != "" {
		client := webhook.NewClient(config, retryConfig, verbose)

		if verbose {
			fmt.Fprintf(os.Stderr, "[WEBHOOK] Sending to %s\n", config.URL)
		}

		// Create a copy of result without webhook fields for sending
		webhookPayload := *result
		webhookPayload.WebhookSent = false
		webhookPayload.WebhookError = ""

		ctx := context.Background()
		if err := client.Send(ctx, &webhookPayload); err != nil {
			// Log webhook error but don't fail the command
			fmt.Fprintf(os.Stderr, "[WEBHOOK] Error: %v\n", err)

			// Add webhook status to result
			result.WebhookSent = false
			result.WebhookError = err.Error()
		} else {
			result.WebhookSent = true
		}
	}

	// Always output to stdout
	return outputJSON(result)
}
