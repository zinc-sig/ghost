package helpers

import (
	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/config"
)

// SetupContextFlags adds context-related flags to a command
func SetupContextFlags(cmd *cobra.Command, cfg *config.ContextConfig) {
	cmd.Flags().StringVar(&cfg.JSON, "context", "", "Context data as JSON string")
	cmd.Flags().StringArrayVar(&cfg.KV, "context-kv", nil, "Context key=value pairs (can be used multiple times)")
	cmd.Flags().StringVar(&cfg.File, "context-file", "", "Path to JSON file containing context data")
}

// SetupUploadFlags adds upload-related flags to a command
func SetupUploadFlags(cmd *cobra.Command, cfg *config.UploadConfig) {
	cmd.Flags().StringVar(&cfg.Provider, "upload-provider", "", "Upload provider type (e.g., minio)")
	cmd.Flags().StringVar(&cfg.Config, "upload-config", "", "Upload configuration as JSON string")
	cmd.Flags().StringArrayVar(&cfg.ConfigKV, "upload-config-kv", nil, "Upload config key=value pairs (can be used multiple times)")
	cmd.Flags().StringVar(&cfg.ConfigFile, "upload-config-file", "", "Path to JSON file containing upload configuration")
	cmd.Flags().StringArrayVar(&cfg.UploadFiles, "upload-files", nil, "Additional files to upload (format: local[:remote], can be used multiple times)")
}

// SetupCommonFlags adds commonly used flags to a command
func SetupCommonFlags(cmd *cobra.Command, flags *config.CommonFlags) {
	cmd.Flags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show command stderr on terminal in addition to file")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "Show what would be executed without running commands")
	cmd.Flags().StringVarP(&flags.TimeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")
	cmd.Flags().StringVar(&flags.Score, "score", "", "Optional score value (included in output if exit code is 0)")
}

// SetupWebhookFlags adds webhook-related flags to a command
func SetupWebhookFlags(cmd *cobra.Command, cfg *config.WebhookConfig) {
	// Direct configuration flags
	cmd.Flags().StringVar(&cfg.URL, "webhook-url", "", "Webhook URL to send results to")
	cmd.Flags().StringVar(&cfg.Method, "webhook-method", DefaultWebhookMethod, "HTTP method to use: GET, POST, PUT, PATCH, DELETE")
	cmd.Flags().StringVar(&cfg.AuthType, "webhook-auth-type", DefaultWebhookAuthType, "Authentication type: none, bearer, api-key")
	cmd.Flags().StringVar(&cfg.AuthToken, "webhook-auth-token", "", "Authentication token (use with --webhook-auth-type)")
	cmd.Flags().IntVar(&cfg.Retries, "webhook-retries", DefaultWebhookRetries, "Maximum webhook retry attempts (0 = no retries)")
	cmd.Flags().StringVar(&cfg.RetryDelay, "webhook-retry-delay", DefaultWebhookRetryDelay, "Initial delay between webhook retries")
	cmd.Flags().StringVar(&cfg.Timeout, "webhook-timeout", DefaultWebhookTimeout, "Total timeout for webhook including retries")

	// Alternative configuration methods
	cmd.Flags().StringVar(&cfg.Config, "webhook-config", "", "Webhook configuration as JSON string")
	cmd.Flags().StringArrayVar(&cfg.ConfigKV, "webhook-config-kv", nil, "Webhook config key=value pairs (can be used multiple times)")
	cmd.Flags().StringVar(&cfg.ConfigFile, "webhook-config-file", "", "Path to JSON file containing webhook configuration")
}
