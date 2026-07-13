//go:build linux

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/zinc-sig/ghost/internal/sandbox"
)

// ExecuteExec replaces the current process with the target command using syscall.Exec.
// It redirects stdin/stdout/stderr via dup3, optionally applies sandbox restrictions,
// and then calls execve. This function does not return on success.
func ExecuteExec(config *Config) error {
	// Open input file and dup3 to stdin
	inputFile, err := os.Open(config.InputFile)
	if err != nil {
		return fmt.Errorf("exec: failed to open input file %s: %w", config.InputFile, err)
	}
	if err := dup3(int(inputFile.Fd()), int(os.Stdin.Fd()), 0); err != nil {
		return fmt.Errorf("exec: dup3 stdin: %w", err)
	}

	// Open/create output file and dup3 to stdout
	outputFile, err := os.Create(config.OutputFile)
	if err != nil {
		return fmt.Errorf("exec: failed to create output file %s: %w", config.OutputFile, err)
	}
	if err := dup3(int(outputFile.Fd()), int(os.Stdout.Fd()), 0); err != nil {
		return fmt.Errorf("exec: dup3 stdout: %w", err)
	}

	// Open/create stderr file and dup3 to stderr
	stderrFile, err := os.Create(config.StderrFile)
	if err != nil {
		return fmt.Errorf("exec: failed to create stderr file %s: %w", config.StderrFile, err)
	}
	if err := dup3(int(stderrFile.Fd()), int(os.Stderr.Fd()), 0); err != nil {
		return fmt.Errorf("exec: dup3 stderr: %w", err)
	}

	// Apply Landlock filesystem restrictions if requested. Network isolation
	// is the container/cluster's responsibility (egress NetworkPolicy), not
	// ghost's.
	if config.Landlock {
		if err := sandbox.ApplySandbox(config.SandboxWorkDir); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
	}

	// Enforce process limit (independent of sandbox — works with --exec alone)
	if config.MaxPids > 0 {
		if err := sandbox.EnforceMaxPids(config.MaxPids); err != nil {
			return fmt.Errorf("exec: failed to enforce max pids: %w", err)
		}
	}

	if config.SeccompProfileJSON != "" {
		if err := sandbox.ApplySeccompFromJSON([]byte(config.SeccompProfileJSON)); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
	}

	// Resolve the command path
	cmdPath, err := exec.LookPath(config.Command)
	if err != nil {
		return fmt.Errorf("exec: command not found: %s: %w", config.Command, err)
	}

	// Build argv (argv[0] is the command name)
	argv := append([]string{config.Command}, config.Args...)

	// Replace the current process
	return syscall.Exec(cmdPath, argv, os.Environ())
}

// dup3 wraps the dup3 syscall. With flags=0 it behaves like dup2.
// dup3 is used instead of dup2 because dup2 is not available on arm64 in Go.
func dup3(oldfd, newfd, flags int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_DUP3, uintptr(oldfd), uintptr(newfd), uintptr(flags))
	if errno != 0 {
		return errno
	}
	return nil
}
