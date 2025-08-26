package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper functions
func createTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	return string(content)
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	content := readFile(t, path)
	if content != expected {
		t.Errorf("file content mismatch\ngot:  %q\nwant: %q", content, expected)
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name          string
		setupConfig   func(t *testing.T, tmpDir string) *Config
		wantExitCode  int
		wantError     bool
		errorContains string
		checkOutput   func(t *testing.T, tmpDir string)
	}{
		{
			name: "successful echo command",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "test input\n")
				return &Config{
					Command:    "echo",
					Args:       []string{"hello world"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				assertFileContains(t, filepath.Join(tmpDir, "output.txt"), "hello world\n")
				assertFileContains(t, filepath.Join(tmpDir, "stderr.txt"), "")
			},
		},
		{
			name: "successful cat command with input",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "content from input")
				return &Config{
					Command:    "cat",
					Args:       []string{},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				assertFileContains(t, filepath.Join(tmpDir, "output.txt"), "content from input")
				assertFileContains(t, filepath.Join(tmpDir, "stderr.txt"), "")
			},
		},
		{
			name: "command with non-zero exit code",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "exit 42"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 42,
			wantError:    false,
		},
		{
			name: "command writes to stderr",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "echo 'error message' >&2"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				assertFileContains(t, filepath.Join(tmpDir, "output.txt"), "")
				assertFileContains(t, filepath.Join(tmpDir, "stderr.txt"), "error message\n")
			},
		},
		{
			name: "non-existent input file",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				return &Config{
					Command:    "echo",
					Args:       []string{"test"},
					InputFile:  filepath.Join(tmpDir, "nonexistent.txt"),
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantError:     true,
			errorContains: "failed to open input file",
		},
		{
			name: "creates parent directories for output",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "echo",
					Args:       []string{"test"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "new", "nested", "dir", "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				// Verify the nested directory structure was created and file contains output
				outputPath := filepath.Join(tmpDir, "new", "nested", "dir", "output.txt")
				assertFileContains(t, outputPath, "test\n")
			},
		},
		{
			name: "creates parent directories for stderr",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "echo 'error' >&2"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "logs", "errors", "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				// Verify stderr directory was created and file contains error output
				stderrPath := filepath.Join(tmpDir, "logs", "errors", "stderr.txt")
				assertFileContains(t, stderrPath, "error\n")
			},
		},
		{
			name: "creates directories for both output and stderr",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "echo 'out' && echo 'err' >&2"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "results", "output", "stdout.txt"),
					StderrFile: filepath.Join(tmpDir, "results", "errors", "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				// Verify both directory structures were created
				outputPath := filepath.Join(tmpDir, "results", "output", "stdout.txt")
				stderrPath := filepath.Join(tmpDir, "results", "errors", "stderr.txt")
				assertFileContains(t, outputPath, "out\n")
				assertFileContains(t, stderrPath, "err\n")
			},
		},
		{
			name: "non-existent command",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "nonexistentcommand12345",
					Args:       []string{},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantError:     true,
			errorContains: "failed to start command",
		},
		{
			name: "command with multiple arguments",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "echo $1 $2 $3", "sh", "arg1", "arg2", "arg3"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				assertFileContains(t, filepath.Join(tmpDir, "output.txt"), "arg1 arg2 arg3\n")
			},
		},
		{
			name: "command that takes time to execute",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "sh",
					Args:       []string{"-c", "sleep 0.1 && echo done"},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
			checkOutput: func(t *testing.T, tmpDir string) {
				assertFileContains(t, filepath.Join(tmpDir, "output.txt"), "done\n")
			},
		},
		{
			name: "false command returns exit code 1",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "false",
					Args:       []string{},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 1,
			wantError:    false,
		},
		{
			name: "true command returns exit code 0",
			setupConfig: func(t *testing.T, tmpDir string) *Config {
				inputFile := createTempFile(t, tmpDir, "input.txt", "")
				return &Config{
					Command:    "true",
					Args:       []string{},
					InputFile:  inputFile,
					OutputFile: filepath.Join(tmpDir, "output.txt"),
					StderrFile: filepath.Join(tmpDir, "stderr.txt"),
				}
			},
			wantExitCode: 0,
			wantError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			config := tt.setupConfig(t, tmpDir)

			result, err := Execute(config)

			// Check error
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check exit code
			if result.ExitCode != tt.wantExitCode {
				t.Errorf("exit code = %d, want %d", result.ExitCode, tt.wantExitCode)
			}

			// Check execution time is reasonable
			if result.ExecutionTime < 0 {
				t.Errorf("execution time should be non-negative, got %d ms", result.ExecutionTime)
			}

			// Check output files
			if tt.checkOutput != nil {
				tt.checkOutput(t, tmpDir)
			}
		})
	}
}

func TestExecutionTime(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := createTempFile(t, tmpDir, "input.txt", "")

	config := &Config{
		Command:    "sh",
		Args:       []string{"-c", "sleep 0.2"},
		InputFile:  inputFile,
		OutputFile: filepath.Join(tmpDir, "output.txt"),
		StderrFile: filepath.Join(tmpDir, "stderr.txt"),
	}

	start := time.Now()
	result, err := Execute(config)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Execution time should be at least 200ms (the sleep duration)
	if result.ExecutionTime < 200 {
		t.Errorf("execution time too short: %d ms, expected at least 200 ms", result.ExecutionTime)
	}

	// Execution time should be close to actual elapsed time
	diff := elapsed - result.ExecutionTime
	if diff < -50 || diff > 50 {
		t.Errorf("execution time %d ms differs significantly from actual elapsed time %d ms",
			result.ExecutionTime, elapsed)
	}
}

func TestLargeOutput(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := createTempFile(t, tmpDir, "input.txt", "")

	// Generate large output
	largeText := strings.Repeat("Hello World\n", 10000)

	config := &Config{
		Command:    "sh",
		Args:       []string{"-c", "for i in $(seq 1 10000); do echo 'Hello World'; done"},
		InputFile:  inputFile,
		OutputFile: filepath.Join(tmpDir, "output.txt"),
		StderrFile: filepath.Join(tmpDir, "stderr.txt"),
	}

	result, err := Execute(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	// Check that output file has expected size
	outputContent := readFile(t, filepath.Join(tmpDir, "output.txt"))
	if len(outputContent) != len(largeText) {
		t.Errorf("output size mismatch: got %d bytes, want %d bytes",
			len(outputContent), len(largeText))
	}
}

func BenchmarkExecute(b *testing.B) {
	tmpDir := b.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	err := os.WriteFile(inputFile, []byte("benchmark input"), 0644)
	if err != nil {
		b.Fatalf("failed to create input file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := &Config{
			Command:    "echo",
			Args:       []string{"benchmark"},
			InputFile:  inputFile,
			OutputFile: filepath.Join(tmpDir, "output.txt"),
			StderrFile: filepath.Join(tmpDir, "stderr.txt"),
		}

		_, err := Execute(config)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
