package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecuteWithTimeout(t *testing.T) {
	tests := []struct {
		name          string
		config        func(dir string) *Config
		wantStatus    Status
		wantExitCode  int
		checkDuration bool
		minDuration   time.Duration
		maxDuration   time.Duration
	}{
		{
			name: "command completes before timeout",
			config: func(dir string) *Config {
				return &Config{
					Command:    "sleep",
					Args:       []string{"0.1"},
					InputFile:  filepath.Join(dir, "input.txt"),
					OutputFile: filepath.Join(dir, "output.txt"),
					StderrFile: filepath.Join(dir, "stderr.txt"),
					Timeout:    1 * time.Second,
				}
			},
			wantStatus:    StatusSuccess,
			wantExitCode:  0,
			checkDuration: true,
			minDuration:   100 * time.Millisecond,
			maxDuration:   500 * time.Millisecond,
		},
		{
			name: "command times out",
			config: func(dir string) *Config {
				return &Config{
					Command:    "sleep",
					Args:       []string{"5"},
					InputFile:  filepath.Join(dir, "input.txt"),
					OutputFile: filepath.Join(dir, "output.txt"),
					StderrFile: filepath.Join(dir, "stderr.txt"),
					Timeout:    100 * time.Millisecond,
				}
			},
			wantStatus:    StatusTimeout,
			wantExitCode:  -1,
			checkDuration: true,
			minDuration:   100 * time.Millisecond,
			maxDuration:   300 * time.Millisecond,
		},
		{
			name: "no timeout specified",
			config: func(dir string) *Config {
				return &Config{
					Command:    "echo",
					Args:       []string{"hello"},
					InputFile:  filepath.Join(dir, "input.txt"),
					OutputFile: filepath.Join(dir, "output.txt"),
					StderrFile: filepath.Join(dir, "stderr.txt"),
					Timeout:    0, // No timeout
				}
			},
			wantStatus:   StatusSuccess,
			wantExitCode: 0,
		},
		{
			name: "command with error and timeout",
			config: func(dir string) *Config {
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "exit 42"},
					InputFile:  filepath.Join(dir, "input.txt"),
					OutputFile: filepath.Join(dir, "output.txt"),
					StderrFile: filepath.Join(dir, "stderr.txt"),
					Timeout:    1 * time.Second,
				}
			},
			wantStatus:   StatusFailed,
			wantExitCode: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			dir, err := os.MkdirTemp("", "test")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()

			// Create input file
			inputFile := filepath.Join(dir, "input.txt")
			if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
				t.Fatal(err)
			}

			config := tt.config(dir)
			startTime := time.Now()
			result, err := Execute(config)
			duration := time.Since(startTime)

			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", result.Status, tt.wantStatus)
			}

			if result.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %v, want %v", result.ExitCode, tt.wantExitCode)
			}

			// Check execution duration if needed
			if tt.checkDuration {
				if duration < tt.minDuration {
					t.Errorf("Execution too fast: %v < %v", duration, tt.minDuration)
				}
				if duration > tt.maxDuration {
					t.Errorf("Execution too slow: %v > %v", duration, tt.maxDuration)
				}
			}

			// Verify full command string
			expectedCommand := config.Command
			if len(config.Args) > 0 {
				expectedCommand = config.Command + " " + config.Args[0]
				if len(config.Args) > 1 {
					for _, arg := range config.Args[1:] {
						expectedCommand = expectedCommand + " " + arg
					}
				}
			}
			if result.Command != expectedCommand {
				t.Errorf("Command = %v, want %v", result.Command, expectedCommand)
			}
		})
	}
}
