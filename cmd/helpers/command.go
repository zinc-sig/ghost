package helpers

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// IOFlags holds the common I/O flags for commands
type IOFlags struct {
	Input    string
	Output   string
	Stderr   string
	Expected string // Optional, only for diff command
}

// ValidateIOFlags validates that required I/O flags are set
func ValidateIOFlags(flags IOFlags, requireExpected bool) error {
	if flags.Input == "" {
		return fmt.Errorf("required flag 'input' not set")
	}
	if flags.Output == "" {
		return fmt.Errorf("required flag 'output' not set")
	}
	if flags.Stderr == "" {
		return fmt.Errorf("required flag 'stderr' not set")
	}
	if requireExpected && flags.Expected == "" {
		return fmt.Errorf("required flag 'expected' not set")
	}
	return nil
}

// CreateTempFiles creates temporary files for output and stderr when upload is configured
// Returns the actual file paths and a cleanup function
func CreateTempFiles(prefix string) (outputFile, stderrFile string, cleanup func(), err error) {
	// Create temp output file
	tempOut, err := os.CreateTemp("", fmt.Sprintf("ghost-%s-output-*.txt", prefix))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create temp output file: %w", err)
	}
	outputFile = tempOut.Name()
	_ = tempOut.Close()

	// Create temp stderr file
	tempErr, err := os.CreateTemp("", fmt.Sprintf("ghost-%s-stderr-*.txt", prefix))
	if err != nil {
		_ = os.Remove(outputFile) // Clean up the first file if second fails
		return "", "", nil, fmt.Errorf("failed to create temp stderr file: %w", err)
	}
	stderrFile = tempErr.Name()
	_ = tempErr.Close()

	// Return cleanup function
	cleanup = func() {
		_ = os.Remove(outputFile)
		_ = os.Remove(stderrFile)
	}

	return outputFile, stderrFile, cleanup, nil
}

// ValidateCommandSeparator checks if the '--' separator is present for run command
func ValidateCommandSeparator(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified after '--'")
	}

	dashIndex := cmd.ArgsLenAtDash()
	if dashIndex == -1 {
		return fmt.Errorf("command separator '--' is required")
	}

	return nil
}

// ParseTimeout parses and validates a timeout duration string
func ParseTimeout(timeoutStr string) (time.Duration, error) {
	if timeoutStr == "" {
		return 0, nil
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout duration: %w", err)
	}

	if timeout <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}

	return timeout, nil
}