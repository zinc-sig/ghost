package runner

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Config struct {
	Command    string
	Args       []string
	InputFile  *string
	OutputFile *string
	StderrFile *string
}

type Result struct {
	ExitCode      int
	ExecutionTime int64 // milliseconds
}

func Execute(config *Config) (*Result, error) {
	cmd := exec.Command(config.Command, config.Args...)

	if config.InputFile != nil {
		inputFile, err := os.Open(*config.InputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open input file %s: %w", *config.InputFile, err)
		}
		defer inputFile.Close()
		cmd.Stdin = inputFile
	}

	if config.OutputFile != nil {
		outputFile, err := os.Create(*config.OutputFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create output file %s: %w", *config.OutputFile, err)
		}
		defer outputFile.Close()
		cmd.Stdout = outputFile
	}

	if config.StderrFile != nil {
		stderrFile, err := os.Create(*config.StderrFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create stderr file %s: %w", *config.StderrFile, err)
		}
		defer stderrFile.Close()
		cmd.Stderr = stderrFile
	}

	startTime := time.Now()
	err := cmd.Run()
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
		ExitCode:      exitCode,
		ExecutionTime: executionTime,
	}, nil
}