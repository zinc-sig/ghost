package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandTimeout(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantStatus   string
		wantExitCode int
		wantErr      bool
	}{
		{
			name: "command completes before timeout",
			args: []string{
				"run", "-i", "input.txt", "-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "1s", "--", "echo", "hello",
			},
			wantStatus:   "success",
			wantExitCode: 0,
			wantErr:      false,
		},
		{
			name: "command times out",
			args: []string{
				"run", "-i", "input.txt", "-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "100ms", "--", "sleep", "5",
			},
			wantStatus:   "timeout",
			wantExitCode: -1,
			wantErr:      false,
		},
		{
			name: "invalid timeout format",
			args: []string{
				"run", "-i", "input.txt", "-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "invalid", "--", "echo", "hello",
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			args: []string{
				"run", "-i", "input.txt", "-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "-1s", "--", "echo", "hello",
			},
			wantErr: true,
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

			// Update args with absolute paths
			for i, arg := range tt.args {
				if arg == "input.txt" || arg == "output.txt" || arg == "stderr.txt" {
					tt.args[i] = filepath.Join(dir, arg)
				}
			}

			// Create input file
			inputFile := filepath.Join(dir, "input.txt")
			if err := os.WriteFile(inputFile, []byte("test input\n"), 0644); err != nil {
				t.Fatal(err)
			}

			// Execute command
			rootCmd.SetArgs(tt.args)
			output, err := captureOutput(func() error {
				return rootCmd.Execute()
			})

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Parse JSON output
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
			}

			// Verify status
			if status, ok := result["status"].(string); !ok || status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", status, tt.wantStatus)
			}

			// Verify exit code
			if exitCode, ok := result["exit_code"].(float64); !ok || int(exitCode) != tt.wantExitCode {
				t.Errorf("ExitCode = %v, want %v", int(exitCode), tt.wantExitCode)
			}

			// Verify timeout field is present when timeout is specified
			if strings.Contains(strings.Join(tt.args, " "), "--timeout") && !tt.wantErr {
				if _, ok := result["timeout"]; !ok {
					t.Errorf("Expected timeout field in output")
				}
			}
		})
	}
}

func TestDiffCommandTimeout(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		file1Content string
		file2Content string
		wantStatus   string
		wantExitCode int
		wantErr      bool
	}{
		{
			name: "diff completes before timeout",
			args: []string{
				"diff", "-i", "file1.txt", "-x", "file2.txt",
				"-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "1s",
			},
			file1Content: "hello\n",
			file2Content: "hello\n",
			wantStatus:   "success",
			wantExitCode: 0,
			wantErr:      false,
		},
		{
			name: "diff with invalid timeout",
			args: []string{
				"diff", "-i", "file1.txt", "-x", "file2.txt",
				"-o", "output.txt", "-e", "stderr.txt",
				"--timeout", "0s",
			},
			file1Content: "hello\n",
			file2Content: "hello\n",
			wantErr:      true,
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

			// Update args with absolute paths
			for i, arg := range tt.args {
				if arg == "file1.txt" || arg == "file2.txt" || arg == "output.txt" || arg == "stderr.txt" {
					tt.args[i] = filepath.Join(dir, arg)
				}
			}

			// Create input files
			file1 := filepath.Join(dir, "file1.txt")
			if err := os.WriteFile(file1, []byte(tt.file1Content), 0644); err != nil {
				t.Fatal(err)
			}

			file2 := filepath.Join(dir, "file2.txt")
			if err := os.WriteFile(file2, []byte(tt.file2Content), 0644); err != nil {
				t.Fatal(err)
			}

			// Execute command
			rootCmd.SetArgs(tt.args)
			output, err := captureOutput(func() error {
				return rootCmd.Execute()
			})

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Parse JSON output
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
			}

			// Verify status
			if status, ok := result["status"].(string); !ok || status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", status, tt.wantStatus)
			}

			// Verify exit code
			if exitCode, ok := result["exit_code"].(float64); !ok || int(exitCode) != tt.wantExitCode {
				t.Errorf("ExitCode = %v, want %v", int(exitCode), tt.wantExitCode)
			}

			// Verify timeout field is present
			if strings.Contains(strings.Join(tt.args, " "), "--timeout") && !tt.wantErr {
				if _, ok := result["timeout"]; !ok {
					t.Errorf("Expected timeout field in output")
				}
			}
		})
	}
}
