package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	// Command-specific I/O flags
	diffInputFile    string
	diffExpectedFile string
	diffOutputFile   string
	diffStderrFile   string
	diffFlags        string

	// Common flag structures
	diffCommonFlags   CommonFlags
	diffContextConfig ContextConfig
	diffUploadConfig  UploadConfig
	diffWebhookConfig WebhookConfig
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

	// Setup upload provider if configured
	provider, uploadConf, err := SetupUploadProvider(&diffUploadConfig)
	if err != nil {
		return err
	}

	// Print upload info in verbose mode
	if provider != nil && diffCommonFlags.Verbose {
		PrintUploadInfo(provider, uploadConf, diffOutputFile, diffStderrFile)
	}

	// Determine actual execution paths
	actualOutputFile := diffOutputFile
	actualStderrFile := diffStderrFile

	if provider != nil {
		// Create temp files for execution when upload is configured
		tempOut, err := os.CreateTemp("", "ghost-diff-output-*.txt")
		if err != nil {
			return fmt.Errorf("failed to create temp output file: %w", err)
		}
		defer func() { _ = os.Remove(tempOut.Name()) }()
		actualOutputFile = tempOut.Name()
		_ = tempOut.Close()

		tempErr, err := os.CreateTemp("", "ghost-diff-stderr-*.txt")
		if err != nil {
			return fmt.Errorf("failed to create temp stderr file: %w", err)
		}
		defer func() { _ = os.Remove(tempErr.Name()) }()
		actualStderrFile = tempErr.Name()
		_ = tempErr.Close()
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
		OutputFile: actualOutputFile,
		StderrFile: actualStderrFile,
		Verbose:    diffCommonFlags.Verbose,
		Timeout:    diffCommonFlags.Timeout,
	}

	// Execute diff command
	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute diff: %w", err)
	}

	// Upload files if provider is configured
	if provider != nil {
		files := map[string]string{
			actualOutputFile: diffOutputFile,
			actualStderrFile: diffStderrFile,
		}
		if err := HandleUploads(provider, files, diffCommonFlags.Verbose); err != nil {
			return err
		}
	}

	// Build context from all sources
	ctx, err := contextparser.BuildContext(diffContextConfig.JSON, diffContextConfig.KV, diffContextConfig.File)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Create JSON result for diff command
	var timeoutMs int64
	if diffCommonFlags.Timeout > 0 {
		timeoutMs = diffCommonFlags.Timeout.Milliseconds()
	}
	jsonResult := createDiffJSONResult(
		diffInputFile,
		diffExpectedFile,
		diffOutputFile,
		diffStderrFile,
		result,
		timeoutMs,
		diffCommonFlags.ScoreSet,
		diffCommonFlags.Score,
		ctx,
	)

	// Output JSON and send webhook
	return outputJSONAndWebhook(jsonResult, diffCommonFlags.Verbose)
}

func init() {
	// Command-specific flags
	diffCmd.Flags().StringVarP(&diffInputFile, "input", "i", "", "Input file to compare (required)")
	diffCmd.Flags().StringVarP(&diffExpectedFile, "expected", "x", "", "Expected file to compare against (required)")
	diffCmd.Flags().StringVarP(&diffOutputFile, "output", "o", "", "Output file for diff results (required)")
	diffCmd.Flags().StringVarP(&diffStderrFile, "stderr", "e", "", "Error file to capture diff's stderr (required)")
	diffCmd.Flags().StringVar(&diffFlags, "diff-flags", "", "Flags to pass to the diff command (e.g., \"--ignore-trailing-space -B\")")

	// Mark flags as required
	_ = diffCmd.MarkFlagRequired("input")
	_ = diffCmd.MarkFlagRequired("expected")
	_ = diffCmd.MarkFlagRequired("output")
	_ = diffCmd.MarkFlagRequired("stderr")

	// Setup common flags using helpers
	SetupCommonFlags(diffCmd, &diffCommonFlags)
	SetupContextFlags(diffCmd, &diffContextConfig)
	SetupUploadFlags(diffCmd, &diffUploadConfig)
	SetupWebhookFlags(diffCmd, &diffWebhookConfig)

	diffCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		diffCommonFlags.ScoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		var err error
		diffCommonFlags.Timeout, err = SetupTimeoutPreRun(diffCommonFlags.TimeoutStr)
		if err != nil {
			return err
		}

		// Parse webhook configuration for diff
		if err := parseDiffWebhookConfig(&diffWebhookConfig); err != nil {
			return err
		}

		return nil
	}
}
