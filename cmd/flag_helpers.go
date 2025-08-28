package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

// SetupContextFlags adds context-related flags to a command
func SetupContextFlags(cmd *cobra.Command, config *ContextConfig) {
	cmd.Flags().StringVar(&config.JSON, "context", "", "Context data as JSON string")
	cmd.Flags().StringArrayVar(&config.KV, "context-kv", nil, "Context key=value pairs (can be used multiple times)")
	cmd.Flags().StringVar(&config.File, "context-file", "", "Path to JSON file containing context data")
}

// SetupUploadFlags adds upload-related flags to a command
func SetupUploadFlags(cmd *cobra.Command, config *UploadConfig) {
	cmd.Flags().StringVar(&config.Provider, "upload-provider", "", "Upload provider type (e.g., minio)")
	cmd.Flags().StringVar(&config.Config, "upload-config", "", "Upload configuration as JSON string")
	cmd.Flags().StringArrayVar(&config.ConfigKV, "upload-config-kv", nil, "Upload config key=value pairs (can be used multiple times)")
	cmd.Flags().StringVar(&config.ConfigFile, "upload-config-file", "", "Path to JSON file containing upload configuration")
}

// SetupCommonFlags adds commonly used flags to a command
func SetupCommonFlags(cmd *cobra.Command, flags *CommonFlags) {
	cmd.Flags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show command stderr on terminal in addition to file")
	cmd.Flags().StringVarP(&flags.TimeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")
	cmd.Flags().IntVar(&flags.Score, "score", 0, "Optional score integer (included in output if exit code is 0)")
}

// SetupTimeoutPreRun is now deprecated - use ParseTimeout from command_helpers.go
// Keeping for backward compatibility
func SetupTimeoutPreRun(timeoutStr string) (time.Duration, error) {
	return ParseTimeout(timeoutStr)
}

// SetupWebhookFlags adds webhook-related flags to a command
func SetupWebhookFlags(cmd *cobra.Command, config *WebhookConfig) {
	cmd.Flags().StringVar(&config.URL, "webhook-url", "", "Webhook URL to send results to")
	cmd.Flags().StringVar(&config.AuthType, "webhook-auth-type", "none", "Authentication type: none, bearer, api-key")
	cmd.Flags().StringVar(&config.AuthToken, "webhook-auth-token", "", "Authentication token (use with --webhook-auth-type)")
	cmd.Flags().IntVar(&config.Retries, "webhook-retries", 3, "Maximum webhook retry attempts (0 = no retries)")
	cmd.Flags().StringVar(&config.RetryDelay, "webhook-retry-delay", "1s", "Initial delay between webhook retries")
	cmd.Flags().StringVar(&config.Timeout, "webhook-timeout", "30s", "Total timeout for webhook including retries")
}
