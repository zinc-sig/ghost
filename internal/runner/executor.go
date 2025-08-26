package runner

import (
	"context"
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
	Command    string
	Args       []string
	InputFile  string
	OutputFile string
	StderrFile string
	Verbose    bool
	Timeout    time.Duration // 0 means no timeout
}

type Result struct {
	Command       string
	Status        Status
	ExitCode      int
	ExecutionTime int64 // milliseconds
}

// createFileWithDir creates a file and any necessary parent directories
func createFileWithDir(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", path, err)
	}
	return file, nil
}

func Execute(config *Config) (*Result, error) {
	// Build the full command string for the result
	fullCommand := config.Command
	if len(config.Args) > 0 {
		fullCommand = fullCommand + " " + strings.Join(config.Args, " ")
	}

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
	if config.Verbose {
		cmd.Stderr = io.MultiWriter(stderrFile, os.Stderr)
	} else {
		cmd.Stderr = stderrFile
	}

	startTime := time.Now()
	err = cmd.Run()
	endTime := time.Now()

	executionTime := endTime.Sub(startTime).Milliseconds()

	// Determine status and exit code based on error
	status := StatusSuccess
	exitCode := 0

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

	return &Result{
		Command:       fullCommand,
		Status:        status,
		ExitCode:      exitCode,
		ExecutionTime: executionTime,
	}, nil
}
