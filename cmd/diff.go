package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	diffInputFile    string
	diffExpectedFile string
	diffOutputFile   string
	diffStderrFile   string
	diffVerbose      bool
	diffTimeoutStr   string
	diffTimeout      time.Duration
	diffFlags        string
	diffScore        int
	diffScoreSet     bool
	diffContextJSON  string
	diffContextKV    []string
	diffContextFile  string

	// Webhook configuration for diff
	diffWebhookURL        string
	diffWebhookAuthType   string
	diffWebhookAuthToken  string
	diffWebhookTimeout    string
	diffWebhookRetries    int
	diffWebhookRetryDelay string
)

var diffCmd = &cobra.Command{
	Use:   "diff -i <input> -x <expected> -o <output> -e <stderr> [--diff-flags <flags>] [--score <value>]",
	Short: "Compare two files with structured output",
	Long: `Compare two files using diff and output the results in JSON format.
Returns exit code 0 if files are identical, 1 if they differ.

The diff output is written to the specified output file, stderr to the stderr file,
and metadata including execution time and optional scoring is returned as JSON.

You can pass additional flags to the diff command using --diff-flags.
Common flags for grading include:
  --ignore-trailing-space (-Z): Ignore white space at line end
  --ignore-space-change (-b): Ignore changes in amount of white space
  --ignore-all-space (-w): Ignore all white space
  --ignore-blank-lines (-B): Ignore changes where lines are all blank`,
	Example: `  ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e errors.txt
  ghost diff -i result.txt -x expected.txt -o diff.txt -e errors.txt --score 100
  ghost diff -i student.txt -x solution.txt -o diff.txt -e errors.txt --diff-flags "--ignore-trailing-space"
  ghost diff -i output.txt -x expected.txt -o diff.txt -e errors.txt --diff-flags "-w -B" --score 100`,
	RunE: diffCommand,
}

func diffCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if diffInputFile == "" {
		return fmt.Errorf("required flag 'input' not set")
	}
	if diffExpectedFile == "" {
		return fmt.Errorf("required flag 'expected' not set")
	}
	if diffOutputFile == "" {
		return fmt.Errorf("required flag 'output' not set")
	}
	if diffStderrFile == "" {
		return fmt.Errorf("required flag 'stderr' not set")
	}

	// Build args for diff command
	var diffArgs []string

	// Add flags if provided
	if diffFlags != "" {
		// Parse the flags string by splitting on whitespace
		flags := strings.Fields(diffFlags)
		diffArgs = append(diffArgs, flags...)
	}

	// Add the file paths
	diffArgs = append(diffArgs, diffInputFile, diffExpectedFile)

	// Build diff command config
	config := &runner.Config{
		Command:    "diff",
		Args:       diffArgs,
		InputFile:  "/dev/null", // diff doesn't need stdin
		OutputFile: diffOutputFile,
		StderrFile: diffStderrFile,
		Verbose:    diffVerbose,
		Timeout:    diffTimeout,
	}

	// Execute diff command
	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute diff: %w", err)
	}

	// Build context from all sources
	ctx, err := contextparser.BuildContext(diffContextJSON, diffContextKV, diffContextFile)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Create JSON result for diff command
	var timeoutMs int64
	if diffTimeout > 0 {
		timeoutMs = diffTimeout.Milliseconds()
	}
	jsonResult := createDiffJSONResult(
		diffInputFile,
		diffExpectedFile,
		diffOutputFile,
		diffStderrFile,
		result,
		timeoutMs,
		diffScoreSet,
		diffScore,
		ctx,
	)

	// Output JSON and send webhook
	return outputJSONAndWebhook(jsonResult, diffVerbose)
}

func init() {
	diffCmd.Flags().StringVarP(&diffInputFile, "input", "i", "", "Input file to compare (required)")
	diffCmd.Flags().StringVarP(&diffExpectedFile, "expected", "x", "", "Expected file to compare against (required)")
	diffCmd.Flags().StringVarP(&diffOutputFile, "output", "o", "", "Output file for diff results (required)")
	diffCmd.Flags().StringVarP(&diffStderrFile, "stderr", "e", "", "Error file to capture diff's stderr (required)")
	diffCmd.Flags().BoolVarP(&diffVerbose, "verbose", "v", false, "Show diff stderr on terminal in addition to file")
	diffCmd.Flags().StringVar(&diffFlags, "diff-flags", "", "Flags to pass to the diff command (e.g., \"--ignore-trailing-space -B\")")
	diffCmd.Flags().StringVarP(&diffTimeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")

	// Mark flags as required
	_ = diffCmd.MarkFlagRequired("input")
	_ = diffCmd.MarkFlagRequired("expected")
	_ = diffCmd.MarkFlagRequired("output")
	_ = diffCmd.MarkFlagRequired("stderr")

	diffCmd.Flags().IntVar(&diffScore, "score", 0, "Optional score integer (included in output if files match)")

	// Context flags
	diffCmd.Flags().StringVar(&diffContextJSON, "context", "", "Context data as JSON string")
	diffCmd.Flags().StringArrayVar(&diffContextKV, "context-kv", nil, "Context key=value pairs (can be used multiple times)")
	diffCmd.Flags().StringVar(&diffContextFile, "context-file", "", "Path to JSON file containing context data")

	// Webhook flags for diff
	diffCmd.Flags().StringVar(&diffWebhookURL, "webhook-url", "", "Webhook URL to send results to")
	diffCmd.Flags().StringVar(&diffWebhookAuthType, "webhook-auth-type", "none", "Authentication type: none, bearer, api-key")
	diffCmd.Flags().StringVar(&diffWebhookAuthToken, "webhook-auth-token", "", "Authentication token (use with --webhook-auth-type)")
	diffCmd.Flags().IntVar(&diffWebhookRetries, "webhook-retries", 3, "Maximum webhook retry attempts (0 = no retries)")
	diffCmd.Flags().StringVar(&diffWebhookRetryDelay, "webhook-retry-delay", "1s", "Initial delay between webhook retries")
	diffCmd.Flags().StringVar(&diffWebhookTimeout, "webhook-timeout", "30s", "Total timeout for webhook including retries")

	diffCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		diffScoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		if diffTimeoutStr != "" {
			var err error
			diffTimeout, err = time.ParseDuration(diffTimeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout duration: %w", err)
			}
			if diffTimeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
		}

		// Parse webhook configuration for diff
		if err := parseDiffWebhookConfig(); err != nil {
			return err
		}

		return nil
	}
}
