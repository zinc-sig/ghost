package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ghost",
	Short: "A command orchestration tool with structured output",
	Long: `Ghost is a CLI tool for executing commands while capturing execution metadata.
It provides structured JSON output with timing information, exit codes, and optional scoring.

Perfect for testing frameworks, CI/CD pipelines, and process automation.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(diffCmd)
}
