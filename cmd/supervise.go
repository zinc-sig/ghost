package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/cmd/helpers"
	"github.com/zinc-sig/ghost/internal/runner"
)

var (
	superviseInputFile  string
	superviseOutputFile string
	superviseStderrFile string

	superviseTimeoutStr         string
	superviseLandlock           bool
	superviseWorkdir            string
	superviseMaxPids            uint64
	superviseMaxOutputBytes     int64
	superviseResultFile         string
	superviseSeccompProfileJSON string
)

var superviseCmd = &cobra.Command{
	Use:   "supervise [flags] -- <command> [args...]",
	Short: "Fork+wait a command and emit a measurement result trailer",
	Long: `Execute a command in a forked child while measuring it from a live parent
(peak memory, OOM attribution, output-size cap), then write a result trailer to
the result file and as a stream frame on stdout. Unlike exec, ghost is not
replaced — it survives the child to measure and report.

Landlock filesystem restrictions (--landlock) are applied as needed. Network
isolation is the container/cluster's responsibility (egress NetworkPolicy), not
ghost's.

The '--' separator is required to distinguish ghost flags from the target command.`,
	Example: `  ghost supervise --landlock -i /dev/null -o out -e err --result-file=/output/.result -- ./prog`,
	RunE:    superviseCommand,
}

func superviseCommand(cmd *cobra.Command, args []string) error {
	if err := helpers.ValidateCommandSeparator(cmd, args); err != nil {
		return err
	}

	ioFlags := helpers.IOFlags{
		Input:  superviseInputFile,
		Output: superviseOutputFile,
		Stderr: superviseStderrFile,
	}
	if err := helpers.ValidateIOFlags(ioFlags, false); err != nil {
		return err
	}

	timeout, err := helpers.ParseTimeout(superviseTimeoutStr)
	if err != nil {
		return err
	}

	config := &runner.Config{
		Command:            args[0],
		Args:               args[1:],
		InputFile:          superviseInputFile,
		OutputFile:         superviseOutputFile,
		StderrFile:         superviseStderrFile,
		Timeout:            timeout,
		Supervise:          true,
		Landlock:           superviseLandlock,
		SandboxWorkDir:     superviseWorkdir,
		MaxPids:            superviseMaxPids,
		MaxOutputBytes:     superviseMaxOutputBytes,
		ResultFile:         superviseResultFile,
		SeccompProfileJSON: superviseSeccompProfileJSON,
	}

	return runner.Supervise(config)
}

func init() {
	superviseCmd.Flags().StringVarP(&superviseInputFile, "input", "i", "", "Input file to redirect to command's stdin (required)")
	superviseCmd.Flags().StringVarP(&superviseOutputFile, "output", "o", "", "Output file to capture command's stdout (required)")
	superviseCmd.Flags().StringVarP(&superviseStderrFile, "stderr", "e", "", "Error file to capture command's stderr (required)")
	_ = superviseCmd.MarkFlagRequired("input")
	_ = superviseCmd.MarkFlagRequired("output")
	_ = superviseCmd.MarkFlagRequired("stderr")

	superviseCmd.Flags().StringVarP(&superviseTimeoutStr, "timeout", "t", "", "Timeout duration (e.g., 30s, 2m, 500ms)")
	superviseCmd.Flags().BoolVar(&superviseLandlock, "landlock", false, "Apply Landlock filesystem restrictions before execution")
	superviseCmd.Flags().StringVar(&superviseWorkdir, "workdir", "", "Working directory for Landlock read-write rules (defaults to current directory)")
	superviseCmd.Flags().Uint64Var(&superviseMaxPids, "max-pids", 0, "Maximum number of processes for the current user (includes ghost itself; 0 = no limit)")
	superviseCmd.Flags().Int64Var(&superviseMaxOutputBytes, "max-output-bytes", 1048576, "Total /output byte cap enforced as output is written")
	superviseCmd.Flags().StringVar(&superviseResultFile, "result-file", "/output/.result", "Path the supervise result trailer is written to")
	superviseCmd.Flags().StringVar(&superviseSeccompProfileJSON, "seccomp-profile-json", "", "Docker-format seccomp profile JSON applied to the command (inline, single-sourced from core)")

	superviseCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if superviseLandlock && superviseWorkdir == "" {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
			superviseWorkdir = wd
		}
		return nil
	}

	rootCmd.AddCommand(superviseCmd)
}
