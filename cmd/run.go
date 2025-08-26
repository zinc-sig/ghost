package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	inputFile  string
	outputFile string
	stderrFile string
	verbose    bool
	score      int
	scoreSet   bool
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
	}

	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	// Create JSON result using common function
	jsonResult := createJSONResult(
		config.InputFile,
		config.OutputFile,
		config.StderrFile,
		result,
		scoreSet,
		score,
	)

	// Output JSON using common function
	return outputJSON(jsonResult)
}

func init() {
	runCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input file to redirect to command's stdin (required)")
	runCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file to capture command's stdout (required)")
	runCmd.Flags().StringVarP(&stderrFile, "stderr", "e", "", "Error file to capture command's stderr (required)")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show command stderr on terminal in addition to file")

	// Mark flags as required
	_ = runCmd.MarkFlagRequired("input")
	_ = runCmd.MarkFlagRequired("output")
	_ = runCmd.MarkFlagRequired("stderr")
	runCmd.Flags().IntVar(&score, "score", 0, "Optional score integer (included in output if exit code is 0)")

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		scoreSet = cmd.Flags().Changed("score")
		return nil
	}
}
