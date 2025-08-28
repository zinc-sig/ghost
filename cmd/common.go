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

// Global webhook configuration variables
var (
	webhookConfig      *webhook.Config
	webhookRetryConfig *webhook.RetryConfig
	diffWebhookConfig  *webhook.Config
	diffRetryConfig    *webhook.RetryConfig
)

// parseWebhookConfig parses webhook configuration for run command
func parseWebhookConfig() error {
	if webhookURL == "" {
		return nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if webhookTimeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(webhookTimeout)
		if err != nil {
			return fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	} else {
		webhookTimeoutDur = 30 * time.Second
	}

	// Parse retry delay
	var retryDelay time.Duration
	if webhookRetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(webhookRetryDelay)
		if err != nil {
			return fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	webhookConfig = &webhook.Config{
		URL:       webhookURL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  webhookAuthType,
		AuthToken: webhookAuthToken,
	}

	webhookRetryConfig = &webhook.RetryConfig{
		MaxRetries:   webhookRetries,
		InitialDelay: retryDelay,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}

	return nil
}

// parseDiffWebhookConfig parses webhook configuration for diff command
func parseDiffWebhookConfig() error {
	if diffWebhookURL == "" {
		return nil // No webhook configured
	}

	// Parse webhook timeout
	var webhookTimeoutDur time.Duration
	if diffWebhookTimeout != "" {
		var err error
		webhookTimeoutDur, err = time.ParseDuration(diffWebhookTimeout)
		if err != nil {
			return fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	} else {
		webhookTimeoutDur = 30 * time.Second
	}

	// Parse retry delay
	var retryDelay time.Duration
	if diffWebhookRetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(diffWebhookRetryDelay)
		if err != nil {
			return fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	} else {
		retryDelay = 1 * time.Second
	}

	diffWebhookConfig = &webhook.Config{
		URL:       diffWebhookURL,
		Method:    "POST",
		Timeout:   webhookTimeoutDur,
		AuthType:  diffWebhookAuthType,
		AuthToken: diffWebhookAuthToken,
	}

	diffRetryConfig = &webhook.RetryConfig{
		MaxRetries:   diffWebhookRetries,
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
		config = diffWebhookConfig
		retryConfig = diffRetryConfig
	} else {
		config = webhookConfig
		retryConfig = webhookRetryConfig
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
