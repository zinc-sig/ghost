package cmd

import (
	"fmt"
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

// SetupTimeoutPreRun parses and validates the timeout duration
func SetupTimeoutPreRun(timeoutStr string) (time.Duration, error) {
	if timeoutStr == "" {
		return 0, nil
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout duration: %w", err)
	}

	if timeout <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}

	return timeout, nil
}

