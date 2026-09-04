// Package cmd defines the ghost command-line interface on Cobra: the run,
// diff, exec, supervise, agent, and heartbeat subcommands, their flag
// validation, and the JSON result written to stdout.
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// appVersion is the ghost build version, injected from main via
// SetVersion (the release workflow sets main.Version with -ldflags).
var appVersion = "dev"

// SetVersion threads the build version into the CLI (and the agent,
// which reports it in the fetch-submission handshake).
func SetVersion(v string) {
	if v == "" {
		return
	}
	appVersion = v
	rootCmd.Version = v
}

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
	rootCmd.AddCommand(heartbeatCmd)
}
