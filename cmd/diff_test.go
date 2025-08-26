package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureOutput captures stdout during function execution
func captureOutput(f func() error) (string, error) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestDiffCommand(t *testing.T) {
	tests := []struct {
		name            string
		setupFiles      func(t *testing.T, tmpDir string) (input, expected string)
		score           int
		useScore        bool
		wantExitCode    int
		wantScore       *int
		checkDiffOutput func(t *testing.T, diffOutput string)
	}{
		{
			name: "identical files",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				content := "Hello, World!\nThis is a test file.\n"
				input := filepath.Join(tmpDir, "input.txt")
				expected := filepath.Join(tmpDir, "expected.txt")

				_ = os.WriteFile(input, []byte(content), 0644)
				_ = os.WriteFile(expected, []byte(content), 0644)

				return input, expected
			},
			useScore:     true,
			score:        100,
			wantExitCode: 0,
			wantScore:    intPtr(100),
			checkDiffOutput: func(t *testing.T, diffOutput string) {
				if diffOutput != "" {
					t.Errorf("Expected empty diff output for identical files, got: %s", diffOutput)
				}
			},
		},
		{
			name: "different files",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				input := filepath.Join(tmpDir, "input.txt")
				expected := filepath.Join(tmpDir, "expected.txt")

				_ = os.WriteFile(input, []byte("Line 1\nLine 2\nLine 3\n"), 0644)
				_ = os.WriteFile(expected, []byte("Line 1\nLine 2 modified\nLine 3\n"), 0644)

				return input, expected
			},
			useScore:     true,
			score:        100,
			wantExitCode: 1,
			wantScore:    intPtr(0),
			checkDiffOutput: func(t *testing.T, diffOutput string) {
				if !strings.Contains(diffOutput, "Line 2") {
					t.Errorf("Expected diff output to contain 'Line 2', got: %s", diffOutput)
				}
			},
		},
		{
			name: "without score flag",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				content := "Same content"
				input := filepath.Join(tmpDir, "input.txt")
				expected := filepath.Join(tmpDir, "expected.txt")

				_ = os.WriteFile(input, []byte(content), 0644)
				_ = os.WriteFile(expected, []byte(content), 0644)

				return input, expected
			},
			useScore:     false,
			wantExitCode: 0,
			wantScore:    nil,
		},
		{
			name: "empty files",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				input := filepath.Join(tmpDir, "input.txt")
				expected := filepath.Join(tmpDir, "expected.txt")

				_ = os.WriteFile(input, []byte(""), 0644)
				_ = os.WriteFile(expected, []byte(""), 0644)

				return input, expected
			},
			useScore:     true,
			score:        50,
			wantExitCode: 0,
			wantScore:    intPtr(50),
		},
		{
			name: "one empty one non-empty",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				input := filepath.Join(tmpDir, "input.txt")
				expected := filepath.Join(tmpDir, "expected.txt")

				_ = os.WriteFile(input, []byte("Content"), 0644)
				_ = os.WriteFile(expected, []byte(""), 0644)

				return input, expected
			},
			useScore:     true,
			score:        75,
			wantExitCode: 1,
			wantScore:    intPtr(0),
			checkDiffOutput: func(t *testing.T, diffOutput string) {
				if !strings.Contains(diffOutput, "Content") {
					t.Errorf("Expected diff output to contain 'Content', got: %s", diffOutput)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			inputFile, expectedFile := tt.setupFiles(t, tmpDir)
			outputFile := filepath.Join(tmpDir, "diff_output.txt")

			// Reset flags
			diffInputFile = inputFile
			diffExpectedFile = expectedFile
			diffOutputFile = outputFile
			diffScoreSet = tt.useScore
			diffScore = tt.score

			// Capture output
			output, err := captureOutput(func() error {
				return diffCommand(diffCmd, []string{})
			})

			if err != nil {
				t.Fatalf("diffCommand returned error: %v", err)
			}

			// Parse JSON output
			var result struct {
				Input         string `json:"input"`
				Output        string `json:"output"`
				Stderr        string `json:"stderr"`
				ExitCode      int    `json:"exit_code"`
				ExecutionTime int64  `json:"execution_time"`
				Score         *int   `json:"score,omitempty"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
			}

			// Check exit code
			if result.ExitCode != tt.wantExitCode {
				t.Errorf("Exit code = %d, want %d", result.ExitCode, tt.wantExitCode)
			}

			// Check score
			if tt.wantScore == nil {
				if result.Score != nil {
					t.Errorf("Score should be nil, got %d", *result.Score)
				}
			} else {
				if result.Score == nil {
					t.Errorf("Score should not be nil, expected %d", *tt.wantScore)
				} else if *result.Score != *tt.wantScore {
					t.Errorf("Score = %d, want %d", *result.Score, *tt.wantScore)
				}
			}

			// Check diff output if provided
			if tt.checkDiffOutput != nil {
				diffContent, err := os.ReadFile(outputFile)
				if err != nil {
					t.Fatalf("Failed to read diff output file: %v", err)
				}
				tt.checkDiffOutput(t, string(diffContent))
			}

			// Verify execution time is reasonable
			if result.ExecutionTime < 0 {
				t.Errorf("Execution time should be non-negative, got %d", result.ExecutionTime)
			}
		})
	}
}

func TestDiffCommandValidation(t *testing.T) {
	tests := []struct {
		name         string
		inputFile    string
		expectedFile string
		outputFile   string
		wantError    string
	}{
		{
			name:         "missing input flag",
			inputFile:    "",
			expectedFile: "expected.txt",
			outputFile:   "output.txt",
			wantError:    "required flag 'input' not set",
		},
		{
			name:         "missing expected flag",
			inputFile:    "input.txt",
			expectedFile: "",
			outputFile:   "output.txt",
			wantError:    "required flag 'expected' not set",
		},
		{
			name:         "missing output flag",
			inputFile:    "input.txt",
			expectedFile: "expected.txt",
			outputFile:   "",
			wantError:    "required flag 'output' not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set flags
			diffInputFile = tt.inputFile
			diffExpectedFile = tt.expectedFile
			diffOutputFile = tt.outputFile

			err := diffCommand(diffCmd, []string{})

			if err == nil {
				t.Fatal("Expected error but got none")
			}

			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("Error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestDiffCommandWithNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create input files
	inputFile := filepath.Join(tmpDir, "input.txt")
	expectedFile := filepath.Join(tmpDir, "expected.txt")
	_ = os.WriteFile(inputFile, []byte("test"), 0644)
	_ = os.WriteFile(expectedFile, []byte("test"), 0644)

	// Use nested directory for output
	outputFile := filepath.Join(tmpDir, "nested", "dirs", "diff_output.txt")

	// Set flags
	diffInputFile = inputFile
	diffExpectedFile = expectedFile
	diffOutputFile = outputFile
	diffScoreSet = false

	// Run command
	_, err := captureOutput(func() error {
		return diffCommand(diffCmd, []string{})
	})

	if err != nil {
		t.Fatalf("diffCommand returned error: %v", err)
	}

	// Verify nested directories were created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Errorf("Output file was not created in nested directory")
	}
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
