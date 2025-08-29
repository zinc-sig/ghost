package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/config"
	"github.com/zinc-sig/ghost/cmd/helpers"
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
	diffCommonFlags   config.CommonFlags
	diffContextConfig config.ContextConfig
	diffUploadConfig  config.UploadConfig
	diffWebhookConfig config.WebhookConfig
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
	// Validate required I/O flags
	ioFlags := helpers.IOFlags{
		Input:    diffInputFile,
		Output:   diffOutputFile,
		Stderr:   diffStderrFile,
		Expected: diffExpectedFile,
	}
	if err := helpers.ValidateIOFlags(ioFlags, true); err != nil {
		return err
	}

	// Setup upload provider if configured
	provider, uploadConf, err := helpers.SetupUploadProvider(&diffUploadConfig, diffCommonFlags.DryRun)
	if err != nil {
		return err
	}

	// Print upload info in verbose or dry run mode
	if provider != nil && (diffCommonFlags.Verbose || diffCommonFlags.DryRun) {
		helpers.PrintUploadInfo(provider, uploadConf, diffOutputFile, diffStderrFile, diffCommonFlags.DryRun)
	}

	// Determine actual execution paths
	actualOutputFile := diffOutputFile
	actualStderrFile := diffStderrFile

	if provider != nil {
		// Create temp files for execution when upload is configured
		tempOut, tempErr, cleanup, err := helpers.CreateTempFiles("diff")
		if err != nil {
			return err
		}
		defer cleanup()
		actualOutputFile = tempOut
		actualStderrFile = tempErr
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
		DryRun:     diffCommonFlags.DryRun,
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
		if err := helpers.HandleUploads(provider, files, diffCommonFlags.Verbose, diffCommonFlags.DryRun); err != nil {
			return err
		}
	}

	// Build context from all sources
	ctx, err := contextparser.BuildContext(diffContextConfig.JSON, diffContextConfig.KV, diffContextConfig.File)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Print context info in dry run mode
	if diffCommonFlags.DryRun && ctx != nil {
		helpers.PrintContextInfo(ctx, true)
	}

	// Create JSON result for diff command
	var timeoutMs int64
	if diffCommonFlags.Timeout > 0 {
		timeoutMs = diffCommonFlags.Timeout.Milliseconds()
	}
	jsonResult := helpers.CreateJSONResult(
		diffInputFile,
		diffOutputFile,
		diffStderrFile,
		diffExpectedFile, // expected path for diff command
		result,
		timeoutMs,
		diffCommonFlags.ScoreSet,
		diffCommonFlags.Score,
		ctx,
	)

	// Output JSON and send webhook
	return helpers.OutputJSONAndWebhook(jsonResult, diffCommonFlags.Verbose, diffCommonFlags.DryRun)
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
	helpers.SetupCommonFlags(diffCmd, &diffCommonFlags)
	helpers.SetupContextFlags(diffCmd, &diffContextConfig)
	helpers.SetupUploadFlags(diffCmd, &diffUploadConfig)
	helpers.SetupWebhookFlags(diffCmd, &diffWebhookConfig)

	diffCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		diffCommonFlags.ScoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		var err error
		diffCommonFlags.Timeout, err = helpers.ParseTimeout(diffCommonFlags.TimeoutStr)
		if err != nil {
			return err
		}

		// Parse webhook configuration for diff
		if err := helpers.ParseWebhookConfig(&diffWebhookConfig, false); err != nil {
			return err
		}

		return nil
	}
}
