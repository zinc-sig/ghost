package cmd

import (
	"context"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/spf13/cobra"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
	"github.com/zinc-sig/ghost/internal/upload"
)

var (
	inputFile   string
	outputFile  string
	stderrFile  string
	verbose     bool
	timeoutStr  string
	timeout     time.Duration
	score       int
	scoreSet    bool
	contextJSON string
	contextKV   []string
	contextFile string

	// Webhook configuration
	webhookURL        string
	webhookAuthType   string
	webhookAuthToken  string
	webhookTimeout    string
	webhookRetries    int
	webhookRetryDelay string

	// Upload flags
	uploadProvider   string
	uploadConfig     string
	uploadConfigKV   []string
	uploadConfigFile string
)

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <command> [args...]",
	Short: "Execute a command with structured output",
	Long: `Execute a command while capturing execution metadata including exit codes,
timing information, and optional scoring. Results are output as JSON.

The '--' separator is required to distinguish ghost flags from the target command.`,
	Example: `  ghost run -i input.txt -o output.txt -e error.log -- ./my-command arg1 arg2
  ghost run -i data.csv -o results.txt -e errors.log --score 85 -- python script.py
  ghost run -i /dev/null -o output.txt -e error.txt -- echo "Hello World"`,
	RunE: runCommand,
}

func runCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified after '--'")
	}

	dashIndex := cmd.ArgsLenAtDash()
	if dashIndex == -1 {
		return fmt.Errorf("command separator '--' is required")
	}

	// Validate required flags
	if inputFile == "" {
		return fmt.Errorf("required flag 'input' not set")
	}
	if outputFile == "" {
		return fmt.Errorf("required flag 'output' not set")
	}
	if stderrFile == "" {
		return fmt.Errorf("required flag 'stderr' not set")
	}

	targetCommand := args[0]
	targetArgs := args[1:]

	// Setup upload provider if configured
	var provider upload.Provider
	if uploadProvider != "" {
		// Parse upload configuration using context parser with custom prefix
		uploadConf, err := buildUploadConfig()
		if err != nil {
			return fmt.Errorf("failed to build upload config: %w", err)
		}

		// Create and configure provider
		provider, err = upload.NewProvider(uploadProvider)
		if err != nil {
			return fmt.Errorf("failed to create upload provider: %w", err)
		}

		if err := provider.Configure(uploadConf); err != nil {
			return fmt.Errorf("failed to configure upload provider: %w", err)
		}
	}

	// Determine actual execution paths
	actualOutputFile := outputFile
	actualStderrFile := stderrFile

	if provider != nil {
		// Create temp files for execution when upload is configured
		tempOut, err := os.CreateTemp("", "ghost-output-*.txt")
		if err != nil {
			return fmt.Errorf("failed to create temp output file: %w", err)
		}
		defer os.Remove(tempOut.Name())
		actualOutputFile = tempOut.Name()
		tempOut.Close()

		tempErr, err := os.CreateTemp("", "ghost-stderr-*.txt")
		if err != nil {
			return fmt.Errorf("failed to create temp stderr file: %w", err)
		}
		defer os.Remove(tempErr.Name())
		actualStderrFile = tempErr.Name()
		tempErr.Close()
	}

	config := &runner.Config{
		Command:    targetCommand,
		Args:       targetArgs,
		InputFile:  inputFile,
		OutputFile: actualOutputFile,
		StderrFile: actualStderrFile,
		Verbose:    verbose,
		Timeout:    timeout,
	}

	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Upload files if provider is configured
	if provider != nil {
		ctx := context.Background()

		// Upload output file
		outputReader, err := os.Open(actualOutputFile)
		if err != nil {
			return fmt.Errorf("failed to open output file for upload: %w", err)
		}
		defer outputReader.Close()

		if err := provider.Upload(ctx, outputReader, outputFile); err != nil {
			return fmt.Errorf("failed to upload output file: %w", err)
		}

		// Upload stderr file
		stderrReader, err := os.Open(actualStderrFile)
		if err != nil {
			return fmt.Errorf("failed to open stderr file for upload: %w", err)
		}
		defer stderrReader.Close()

		if err := provider.Upload(ctx, stderrReader, stderrFile); err != nil {
			return fmt.Errorf("failed to upload stderr file: %w", err)
		}
	}

	// Build context from all sources
	ctxData, err := contextparser.BuildContext(contextJSON, contextKV, contextFile)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Create JSON result using common function
	var timeoutMs int64
	if timeout > 0 {
		timeoutMs = timeout.Milliseconds()
	}
	jsonResult := createJSONResult(
		config.InputFile,
		config.OutputFile,
		config.StderrFile,
		result,
		timeoutMs,
		scoreSet,
		score,
		ctxData,
	)

	// Output JSON and send webhook using common function
	return outputJSONAndWebhook(jsonResult, verbose)
}

// buildUploadConfig builds upload configuration from all sources
func buildUploadConfig() (map[string]any, error) {
	// Parse environment variables with GHOST_UPLOAD_CONFIG prefix
	uploadEnv := parseUploadEnv()

	// Use context parser to build config from all sources
	contexts := []any{}

	// 1. Environment variables (lowest priority)
	if uploadEnv != nil {
		contexts = append(contexts, uploadEnv)
	}

	// 2. Config file
	if uploadConfigFile != "" {
		fileConfig, err := contextparser.ParseFile(uploadConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to parse upload config file: %w", err)
		}
		contexts = append(contexts, fileConfig)
	}

	// 3. JSON string
	if uploadConfig != "" {
		jsonConfig, err := contextparser.ParseJSON(uploadConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse upload config JSON: %w", err)
		}
		contexts = append(contexts, jsonConfig)
	}

	// 4. Key-value pairs (highest priority)
	if len(uploadConfigKV) > 0 {
		kvConfig := make(map[string]any)
		for _, kv := range uploadConfigKV {
			key, value, err := contextparser.ParseKV(kv)
			if err != nil {
				return nil, fmt.Errorf("failed to parse upload config KV: %w", err)
			}
			kvConfig[key] = value
		}
		contexts = append(contexts, kvConfig)
	}

	result := contextparser.MergeContexts(contexts...)
	if result == nil {
		return make(map[string]any), nil
	}

	if m, ok := result.(map[string]any); ok {
		return m, nil
	}

	return nil, fmt.Errorf("upload config must be an object/map")
}

// parseUploadEnv parses GHOST_UPLOAD_CONFIG* environment variables
func parseUploadEnv() map[string]any {
	config := make(map[string]any)

	// Check for GHOST_UPLOAD_CONFIG JSON string
	if jsonStr := os.Getenv("GHOST_UPLOAD_CONFIG"); jsonStr != "" {
		if parsed, err := contextparser.ParseJSON(jsonStr); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				maps.Copy(config, m)
			}
		}
	}

	// Check for GHOST_UPLOAD_CONFIG_* variables
	environ := os.Environ()
	for _, env := range environ {
		const prefix = "GHOST_UPLOAD_CONFIG_"
		if len(env) > len(prefix) && env[:len(prefix)] == prefix {
			parts := [2]string{}
			idx := 0
			for i, ch := range env {
				if ch == '=' && idx == 0 {
					parts[0] = env[:i]
					parts[1] = env[i+1:]
					idx = 1
					break
				}
			}
			if idx == 1 && len(parts[1]) > 0 {
				key := parts[0][len(prefix):]
				key = toLowerSnakeCase(key)
				// Apply type inference to env var values
				_, value, _ := contextparser.ParseKV(key + "=" + parts[1])
				config[key] = value
			}
		}
	}

	if len(config) == 0 {
		return nil
	}
	return config
}

// toLowerSnakeCase converts UPPER_SNAKE_CASE to lower_snake_case
func toLowerSnakeCase(s string) string {
	result := ""
	for i, ch := range s {
		if ch == '_' {
			result += "_"
		} else if ch >= 'A' && ch <= 'Z' {
			result += string(ch - 'A' + 'a')
		} else {
			result += string(ch)
		}
		_ = i
	}
	return result
}

func init() {
	runCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input file to redirect to command's stdin (required)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file to capture command's stdout (required)")
	runCmd.Flags().StringVarP(&stderrFile, "stderr", "e", "", "Error file to capture command's stderr (required)")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show command stderr on terminal in addition to file")
	runCmd.Flags().StringVarP(&timeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")

	// Mark flags as required
	_ = runCmd.MarkFlagRequired("input")
	_ = runCmd.MarkFlagRequired("output")
	_ = runCmd.MarkFlagRequired("stderr")
	runCmd.Flags().IntVar(&score, "score", 0, "Optional score integer (included in output if exit code is 0)")

	// Context flags
	runCmd.Flags().StringVar(&contextJSON, "context", "", "Context data as JSON string")
	runCmd.Flags().StringArrayVar(&contextKV, "context-kv", nil, "Context key=value pairs (can be used multiple times)")
	runCmd.Flags().StringVar(&contextFile, "context-file", "", "Path to JSON file containing context data")

	// Webhook flags
	runCmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Webhook URL to send results to")
	runCmd.Flags().StringVar(&webhookAuthType, "webhook-auth-type", "none", "Authentication type: none, bearer, api-key")
	runCmd.Flags().StringVar(&webhookAuthToken, "webhook-auth-token", "", "Authentication token (use with --webhook-auth-type)")
	runCmd.Flags().IntVar(&webhookRetries, "webhook-retries", 3, "Maximum webhook retry attempts (0 = no retries)")
	runCmd.Flags().StringVar(&webhookRetryDelay, "webhook-retry-delay", "1s", "Initial delay between webhook retries")
	runCmd.Flags().StringVar(&webhookTimeout, "webhook-timeout", "30s", "Total timeout for webhook including retries")

	// Upload flags
	runCmd.Flags().StringVar(&uploadProvider, "upload-provider", "", "Upload provider type (e.g., minio)")
	runCmd.Flags().StringVar(&uploadConfig, "upload-config", "", "Upload configuration as JSON string")
	runCmd.Flags().StringArrayVar(&uploadConfigKV, "upload-config-kv", nil, "Upload config key=value pairs (can be used multiple times)")
	runCmd.Flags().StringVar(&uploadConfigFile, "upload-config-file", "", "Path to JSON file containing upload configuration")

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		scoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		if timeoutStr != "" {
			var err error
			timeout, err = time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout duration: %w", err)
			}
			if timeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
		}

		// Parse webhook configuration
		if err := parseWebhookConfig(); err != nil {
			return err
		}

		return nil
	}
}
