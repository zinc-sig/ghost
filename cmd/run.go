package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/runner"
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

	config := &runner.Config{
		Command:    targetCommand,
		Args:       targetArgs,
		InputFile:  inputFile,
		OutputFile: outputFile,
		StderrFile: stderrFile,
		Verbose:    verbose,
		Timeout:    timeout,
	}

	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Build context from all sources
	ctx, err := contextparser.BuildContext(contextJSON, contextKV, contextFile)
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
		ctx,
	)

	// Output JSON using common function
	return outputJSON(jsonResult)
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

		return nil
	}
}
