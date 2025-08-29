package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/config"
	"github.com/zinc-sig/ghost/cmd/helpers"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	// Command-specific I/O flags
	inputFile  string
	outputFile string
	stderrFile string

	// Common flag structures
	runFlags         config.CommonFlags
	runContextConfig config.ContextConfig
	runUploadConfig  config.UploadConfig
	runWebhookConfig config.WebhookConfig
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
	// Validate command separator
	if err := helpers.ValidateCommandSeparator(cmd, args); err != nil {
		return err
	}

	// Validate required I/O flags
	ioFlags := helpers.IOFlags{
		Input:  inputFile,
		Output: outputFile,
		Stderr: stderrFile,
	}
	if err := helpers.ValidateIOFlags(ioFlags, false); err != nil {
		return err
	}

	targetCommand := args[0]
	targetArgs := args[1:]

	// Setup upload provider if configured
	provider, uploadConf, err := helpers.SetupUploadProvider(&runUploadConfig, runFlags.DryRun)
	if err != nil {
		return err
	}

	// Parse additional upload files if specified
	var additionalFiles map[string]string
	if len(runUploadConfig.UploadFiles) > 0 {
		additionalFiles, err = helpers.ParseUploadFiles(runUploadConfig.UploadFiles)
		if err != nil {
			return fmt.Errorf("failed to parse upload files: %w", err)
		}
	}

	// Print upload info in verbose or dry run mode
	if provider != nil && (runFlags.Verbose || runFlags.DryRun) {
		helpers.PrintUploadInfo(provider, uploadConf, outputFile, stderrFile, additionalFiles, runFlags.DryRun)
	}

	// Determine actual execution paths
	actualOutputFile := outputFile
	actualStderrFile := stderrFile

	if provider != nil {
		// Create temp files for execution when upload is configured
		tempOut, tempErr, cleanup, err := helpers.CreateTempFiles("run")
		if err != nil {
			return err
		}
		defer cleanup()
		actualOutputFile = tempOut
		actualStderrFile = tempErr
	}

	config := &runner.Config{
		Command:    targetCommand,
		Args:       targetArgs,
		InputFile:  inputFile,
		OutputFile: actualOutputFile,
		StderrFile: actualStderrFile,
		Verbose:    runFlags.Verbose,
		DryRun:     runFlags.DryRun,
		Timeout:    runFlags.Timeout,
	}

	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Upload files if provider is configured
	if provider != nil {
		// Validate additional files exist after command execution
		if additionalFiles != nil && !runFlags.DryRun {
			if err := helpers.ValidateUploadFiles(additionalFiles); err != nil {
				return err
			}
		}

		files := map[string]string{
			actualOutputFile: outputFile,
			actualStderrFile: stderrFile,
		}
		if err := helpers.HandleUploads(provider, files, additionalFiles, runFlags.Verbose, runFlags.DryRun); err != nil {
			return err
		}
	}

	// Build context from all sources
	ctxData, err := contextparser.BuildContext(runContextConfig.JSON, runContextConfig.KV, runContextConfig.File)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Print context info in dry run mode
	if runFlags.DryRun && ctxData != nil {
		helpers.PrintContextInfo(ctxData, true)
	}

	// Create JSON result using common function
	var timeoutMs int64
	if runFlags.Timeout > 0 {
		timeoutMs = runFlags.Timeout.Milliseconds()
	}
	jsonResult := helpers.CreateJSONResult(
		config.InputFile,
		config.OutputFile,
		config.StderrFile,
		"", // No expected file for run command
		result,
		timeoutMs,
		runFlags.ScoreSet,
		runFlags.Score,
		ctxData,
	)

	// Output JSON and send webhook using common function
	return helpers.OutputJSONAndWebhook(jsonResult, runFlags.Verbose, runFlags.DryRun)
}

func init() {
	// Command-specific flags
	runCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input file to redirect to command's stdin (required)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file to capture command's stdout (required)")
	runCmd.Flags().StringVarP(&stderrFile, "stderr", "e", "", "Error file to capture command's stderr (required)")

	// Mark flags as required
	_ = runCmd.MarkFlagRequired("input")
	_ = runCmd.MarkFlagRequired("output")
	_ = runCmd.MarkFlagRequired("stderr")

	// Setup common flags using helper
	helpers.SetupCommonFlags(runCmd, &runFlags)
	helpers.SetupContextFlags(runCmd, &runContextConfig)
	helpers.SetupUploadFlags(runCmd, &runUploadConfig)
	helpers.SetupWebhookFlags(runCmd, &runWebhookConfig)

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		runFlags.ScoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		var err error
		runFlags.Timeout, err = helpers.ParseTimeout(runFlags.TimeoutStr)
		if err != nil {
			return err
		}

		// Parse webhook configuration
		if err := helpers.ParseWebhookConfig(&runWebhookConfig, true); err != nil {
			return err
		}

		return nil
	}
}
