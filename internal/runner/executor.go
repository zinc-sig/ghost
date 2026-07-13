package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Status represents the execution status of a command
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
)

type Config struct {
	Command            string
	Args               []string
	InputFile          string
	OutputFile         string
	StderrFile         string
	Verbose            bool
	DryRun             bool
	Timeout            time.Duration // 0 means no timeout
	Sandbox            bool          // legacy bundled Landlock+netns (run default mode)
	Landlock           bool          // apply Landlock filesystem restrictions (exec/supervise)
	Exec               bool
	Supervise          bool
	SandboxWorkDir     string
	MaxPids            uint64
	SeccompProfileJSON string // Docker-format seccomp profile JSON (inline, single-sourced from core)
	MaxOutputBytes     int64  // total /output byte cap for supervise mode
	ResultFile         string // path the supervise trailer is written to
}

type Result struct {
	Command       string
	Status        Status
	ExitCode      int
	ExecutionTime int64 // milliseconds
}

// tightenToOwnerOnly best-effort restricts f to 0600 via fchmod on the OPEN
// DESCRIPTOR (fchmod, not a path-based chmod, so there is no TOCTOU on the name).
// It exists because O_CREAT's mode argument applies only when the file is
// created; a file that already existed keeps its prior, possibly broader, mode.
//
// OWNERSHIP CAVEAT: a process may fchmod only a file it owns (absent CAP_FOWNER).
// In the sandbox the driver's init step pre-creates /output/{stdout,stderr,
// .heartbeat,.result} as ROOT-owned 0666 while ghost runs as a NON-root uid, so
// fchmod there returns EPERM — tightening a root-owned file is core's job, not
// ghost's. We therefore tolerate EPERM/ENOSYS as a no-op (not a run failure) and
// surface any other error. When ghost DOES own the file (standalone use, or a
// re-run over a ghost-created file) the result is guaranteed 0600.
func tightenToOwnerOnly(f *os.File) error {
	if err := f.Chmod(0o600); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOSYS) {
			return nil
		}
		return fmt.Errorf("tighten %s to 0600: %w", f.Name(), err)
	}
	return nil
}

// createFileWithDir creates a file and any necessary parent directories
func createFileWithDir(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	// 0600, not os.Create's 0666: sandbox output files should not be
	// world/group-writable. The Docker read path (CopyFromContainer) runs as the
	// daemon/root and reads 0600 regardless of owner.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", path, err)
	}
	// O_CREAT's 0600 only applies on creation, so a PRE-EXISTING file keeps its
	// old (possibly broader) mode. Tighten it down. Ownership caveat: see
	// tightenToOwnerOnly — a root-owned bind-mount file stays as core made it.
	if err := tightenToOwnerOnly(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func Execute(config *Config) (*Result, error) {
	// Build the full command string for the result
	fullCommand := config.Command
	if len(config.Args) > 0 {
		fullCommand = fullCommand + " " + strings.Join(config.Args, " ")
	}

	// Force verbose when in dry run mode
	verbose := config.Verbose || config.DryRun

	// Print pre-execution context
	if verbose {
		PrintPreExecution(fullCommand, config)
	}

	var executionTime int64
	var status Status
	var exitCode int

	if config.DryRun {
		// Simulate successful execution for dry run
		executionTime = 0
		status = StatusSuccess
		exitCode = 0
	} else {
		// Create command with or without timeout
		var cmd *exec.Cmd
		var ctx context.Context
		var cancel context.CancelFunc

		if config.Timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), config.Timeout)
			defer cancel()
			cmd = exec.CommandContext(ctx, config.Command, config.Args...)
		} else {
			cmd = exec.Command(config.Command, config.Args...)
		}

		inputFile, err := os.Open(config.InputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open input file %s: %w", config.InputFile, err)
		}
		defer func() { _ = inputFile.Close() }()
		cmd.Stdin = inputFile

		outputFile, err := createFileWithDir(config.OutputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create output file: %w", err)
		}
		defer func() { _ = outputFile.Close() }()
		cmd.Stdout = outputFile

		stderrFile, err := createFileWithDir(config.StderrFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr file: %w", err)
		}
		defer func() { _ = stderrFile.Close() }()

		// If verbose mode is enabled, pipe stderr to both file and terminal
		if verbose {
			cmd.Stderr = io.MultiWriter(stderrFile, os.Stderr)
		} else {
			cmd.Stderr = stderrFile
		}

		// Apply sandbox restrictions before running the command
		if config.Sandbox {
			if err := applySandboxToCmd(cmd, config.SandboxWorkDir); err != nil {
				return nil, fmt.Errorf("failed to apply sandbox: %w", err)
			}
		}

		startTime := time.Now()
		err = cmd.Run()
		endTime := time.Now()

		executionTime = endTime.Sub(startTime).Milliseconds()

		// Determine status and exit code based on error
		status = StatusSuccess
		exitCode = 0

		if err != nil {
			// Check for timeout - need to check context directly since exec.ExitError can mask it
			if ctx != nil && ctx.Err() == context.DeadlineExceeded {
				status = StatusTimeout
				exitCode = -1 // Standard exit code for killed process
			} else if exitError, ok := err.(*exec.ExitError); ok {
				status = StatusFailed
				if sysStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
					exitCode = sysStatus.ExitStatus()
				} else {
					exitCode = 1
				}
			} else {
				return nil, fmt.Errorf("failed to start command: %w", err)
			}
		}
	}

	// Print post-execution status
	if verbose {
		PrintPostExecution(status, exitCode, executionTime, config.DryRun)
	}

	return &Result{
		Command:       fullCommand,
		Status:        status,
		ExitCode:      exitCode,
		ExecutionTime: executionTime,
	}, nil
}
