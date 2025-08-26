package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	Command    string
	Args       []string
	InputFile  string
	OutputFile string
	StderrFile string
	Verbose    bool
}

type Result struct {
	Command       string
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
	cmd := exec.Command(config.Command, config.Args...)

	// Build the full command string for the result
	fullCommand := config.Command
	if len(config.Args) > 0 {
		fullCommand = fullCommand + " " + strings.Join(config.Args, " ")
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

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1
			}
		} else {
			return nil, fmt.Errorf("failed to start command: %w", err)
		}
	}

	return &Result{
		Command:       fullCommand,
		ExitCode:      exitCode,
		ExecutionTime: executionTime,
	}, nil
}
