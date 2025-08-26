package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	diffInputFile    string
	diffExpectedFile string
	diffOutputFile   string
	diffScore        int
	diffScoreSet     bool
)

var diffCmd = &cobra.Command{
	Use:   "diff -i <input> -e <expected> -o <output> [--score <value>]",
	Short: "Compare two files with structured output",
	Long: `Compare two files using diff and output the results in JSON format.
Returns exit code 0 if files are identical, 1 if they differ.

The diff output is written to the specified output file, and metadata
including execution time and optional scoring is returned as JSON.`,
	Example: `  ghost diff -i actual.txt -e expected.txt -o diff_output.txt
  ghost diff -i result.txt -e expected.txt -o diff.txt --score 100`,
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

	// Create stderr file path for diff error messages
	stderrFile := diffOutputFile + ".stderr"

	// Build diff command config
	config := &runner.Config{
		Command:    "diff",
		Args:       []string{diffInputFile, diffExpectedFile},
		InputFile:  "/dev/null", // diff doesn't need stdin
		OutputFile: diffOutputFile,
		StderrFile: stderrFile,
	}

	// Execute diff command
	result, err := runner.Execute(config)
	if err != nil {
		return fmt.Errorf("failed to execute diff: %w", err)
	}

	// Create JSON result
	jsonResult := createJSONResult(
		diffInputFile,    // Using input file as the "input" field
		diffExpectedFile, // Using expected file as the "output" field (for comparison reference)
		diffOutputFile,   // The diff output as the "stderr" field (actual diff result)
		result,
		diffScoreSet,
		diffScore,
	)

	// Output JSON
	return outputJSON(jsonResult)
}

func init() {
	diffCmd.Flags().StringVarP(&diffInputFile, "input", "i", "", "Input file to compare (required)")
	diffCmd.Flags().StringVarP(&diffExpectedFile, "expected", "e", "", "Expected file to compare against (required)")
	diffCmd.Flags().StringVarP(&diffOutputFile, "output", "o", "", "Output file for diff results (required)")

	// Mark flags as required
	_ = diffCmd.MarkFlagRequired("input")
	_ = diffCmd.MarkFlagRequired("expected")
	_ = diffCmd.MarkFlagRequired("output")

	diffCmd.Flags().IntVar(&diffScore, "score", 0, "Optional score integer (included in output if files match)")

	diffCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		diffScoreSet = cmd.Flags().Changed("score")
		return nil
	}
}
