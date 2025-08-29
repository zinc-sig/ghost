package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/zinc-sig/ghost/cmd/config"
	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/runner"
	"github.com/zinc-sig/ghost/internal/webhook"
)

// createJSONResult creates a JSON result from execution results
// The expectedPath parameter is optional - pass empty string for run command
func CreateJSONResult(inputPath, outputPath, stderrPath, expectedPath string, result *runner.Result, timeoutMs int64, scoreSet bool, score int, context any) *output.Result {
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

	// Add expected field only if provided (for diff command)
	if expectedPath != "" {
		jsonResult.Expected = &expectedPath
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
func OutputJSON(result *output.Result) error {
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

// ResetWebhookConfigs resets the global webhook configurations (for testing)
func ResetWebhookConfigs() {
	runWebhookConfigParsed = nil
	runRetryConfig = nil
	diffWebhookConfigParsed = nil
	diffRetryConfig = nil
}

// ParseWebhookConfig parses webhook configuration for the specified command
func ParseWebhookConfig(config *config.WebhookConfig, isRunCommand bool) error {
	// Parse to internal structures (BuildWebhookConfig is called inside)
	webhookConfig, retryConfig, err := ParseWebhookConfigToInternal(config)
	if err != nil {
		return err
	}

	// Store in appropriate global variables based on command type
	if isRunCommand {
		runWebhookConfigParsed = webhookConfig
		runRetryConfig = retryConfig
	} else {
		diffWebhookConfigParsed = webhookConfig
		diffRetryConfig = retryConfig
	}

	return nil
}

// outputJSONAndWebhook outputs JSON to stdout and optionally sends to webhook
func OutputJSONAndWebhook(result *output.Result, verbose bool) error {
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
	return OutputJSON(result)
}
