package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	// Command-specific I/O flags
	inputFile  string
	outputFile string
	stderrFile string

	// Common flag structures
	runFlags         CommonFlags
	runContextConfig ContextConfig
	runUploadConfig  UploadConfig
	runWebhookConfig WebhookConfig
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
	if err := ValidateCommandSeparator(cmd, args); err != nil {
		return err
	}

	// Validate required I/O flags
	ioFlags := IOFlags{
		Input:  inputFile,
		Output: outputFile,
		Stderr: stderrFile,
	}
	if err := ValidateIOFlags(ioFlags, false); err != nil {
		return err
	}

	targetCommand := args[0]
	targetArgs := args[1:]

	// Setup upload provider if configured
	provider, uploadConf, err := SetupUploadProvider(&runUploadConfig)
	if err != nil {
		return err
	}

	// Print upload info in verbose mode
	if provider != nil && runFlags.Verbose {
		PrintUploadInfo(provider, uploadConf, outputFile, stderrFile)
	}

	// Determine actual execution paths
	actualOutputFile := outputFile
	actualStderrFile := stderrFile

	if provider != nil {
		// Create temp files for execution when upload is configured
		tempOut, tempErr, cleanup, err := CreateTempFiles("run")
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
		Timeout:    runFlags.Timeout,
	}

	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Upload files if provider is configured
	if provider != nil {
		files := map[string]string{
			actualOutputFile: outputFile,
			actualStderrFile: stderrFile,
		}
		if err := HandleUploads(provider, files, runFlags.Verbose); err != nil {
			return err
		}
	}

	// Build context from all sources
	ctxData, err := contextparser.BuildContext(runContextConfig.JSON, runContextConfig.KV, runContextConfig.File)
	if err != nil {
		return fmt.Errorf("failed to build context: %w", err)
	}

	// Create JSON result using common function
	var timeoutMs int64
	if runFlags.Timeout > 0 {
		timeoutMs = runFlags.Timeout.Milliseconds()
	}
	jsonResult := createJSONResult(
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
	return outputJSONAndWebhook(jsonResult, runFlags.Verbose)
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
	SetupCommonFlags(runCmd, &runFlags)
	SetupContextFlags(runCmd, &runContextConfig)
	SetupUploadFlags(runCmd, &runUploadConfig)
	SetupWebhookFlags(runCmd, &runWebhookConfig)

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		runFlags.ScoreSet = cmd.Flags().Changed("score")

		// Parse timeout if provided
		var err error
		runFlags.Timeout, err = SetupTimeoutPreRun(runFlags.TimeoutStr)
		if err != nil {
			return err
		}

		// Parse webhook configuration
		if err := parseWebhookConfig(&runWebhookConfig); err != nil {
			return err
		}

		return nil
	}
}
