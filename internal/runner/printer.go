package runner

import (
	"fmt"
	"os"
)

// PrintPreExecution prints command details before execution
func PrintPreExecution(fullCommand string, config *Config) {
	header := "Ghost Command Execution Details"
	if config.DryRun {
		header = "Ghost Command Execution Details (DRY RUN)"
	}

	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, header)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintf(os.Stderr, "Command: %s\n", fullCommand)
	fmt.Fprintf(os.Stderr, "Input:   %s\n", config.InputFile)
	fmt.Fprintf(os.Stderr, "Output:  %s\n", config.OutputFile)
	fmt.Fprintf(os.Stderr, "Stderr:  %s\n", config.StderrFile)
	if config.Timeout > 0 {
		fmt.Fprintf(os.Stderr, "Timeout: %s\n", config.Timeout)
	}
	fmt.Fprintln(os.Stderr, "----------------------------------------")

	if config.DryRun {
		fmt.Fprintln(os.Stderr, "[DRY RUN] Command would be executed here")
		fmt.Fprintln(os.Stderr, "----------------------------------------")
	} else {
		fmt.Fprintln(os.Stderr, "Command Output:")
		fmt.Fprintln(os.Stderr, "----------------------------------------")
	}
}

// PrintPostExecution prints execution results after command completion
func PrintPostExecution(status Status, exitCode int, executionTime int64, dryRun bool) {
	fmt.Fprintln(os.Stderr, "----------------------------------------")
	if dryRun {
		fmt.Fprintln(os.Stderr, "Execution Results (DRY RUN - Simulated):")
	} else {
		fmt.Fprintln(os.Stderr, "Execution Results:")
	}
	fmt.Fprintln(os.Stderr, "----------------------------------------")
	fmt.Fprintf(os.Stderr, "Status:         %s\n", status)
	fmt.Fprintf(os.Stderr, "Exit Code:      %d\n", exitCode)
	fmt.Fprintf(os.Stderr, "Execution Time: %d ms\n", executionTime)
	fmt.Fprintln(os.Stderr, "========================================")
}

// ExecutionDetails holds the information for execution printing
type ExecutionDetails struct {
	FullCommand   string
	Config        *Config
	Status        Status
	ExitCode      int
	ExecutionTime int64
}

// PrintExecutionSummary prints a complete execution summary (alternative approach)
// This could be used if you want a single function call for all printing
func PrintExecutionSummary(details *ExecutionDetails, phase string) {
	switch phase {
	case "pre":
		PrintPreExecution(details.FullCommand, details.Config)
	case "post":
		PrintPostExecution(details.Status, details.ExitCode, details.ExecutionTime, details.Config.DryRun)
	}
}
