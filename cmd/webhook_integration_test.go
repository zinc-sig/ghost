package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zinc-sig/ghost/internal/output"
)

// resetWebhookGlobals resets all webhook-related global variables
func resetWebhookGlobals() {
	webhookURL = ""
	webhookAuthType = "none"
	webhookAuthToken = ""
	webhookTimeout = "30s"
	webhookRetries = 3
	webhookRetryDelay = "1s"
	webhookConfig = nil
	webhookRetryConfig = nil

	diffWebhookURL = ""
	diffWebhookAuthType = "none"
	diffWebhookAuthToken = ""
	diffWebhookTimeout = "30s"
	diffWebhookRetries = 3
	diffWebhookRetryDelay = "1s"
	diffWebhookConfig = nil
	diffRetryConfig = nil

	// Reset timeout-related variables
	timeout = 0
	timeoutStr = ""
	diffTimeout = 0
	diffTimeoutStr = ""
}

func TestRunCommand_WithWebhook(t *testing.T) {
	resetWebhookGlobals()
	// Create temporary directory for test files
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	outputFile := filepath.Join(tmpDir, "output.txt")
	stderrFile := filepath.Join(tmpDir, "stderr.txt")

	// Create input file
	if err := os.WriteFile(inputFile, []byte("test input\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create test webhook server
	var receivedPayload output.Result
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		// Read and parse body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read body: %v", err)
		}

		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			t.Errorf("Failed to unmarshal payload: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Reset global variables
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Capture stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Setup command
	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(runCmd)

	// Set flags
	args := []string{
		"run",
		"-i", inputFile,
		"-o", outputFile,
		"-e", stderrFile,
		"--webhook-url", server.URL,
		"--webhook-retries", "0",
		"--",
		"echo", "test output",
	}

	rootCmd.SetArgs(args)

	// Execute command
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	// Close writer and read output
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Parse stdout JSON
	var stdoutResult output.Result
	if err := json.Unmarshal(buf.Bytes(), &stdoutResult); err != nil {
		t.Fatalf("Failed to parse stdout JSON: %v", err)
	}

	// Verify webhook was sent
	if !stdoutResult.WebhookSent {
		t.Error("Expected webhook_sent to be true")
	}

	// Verify webhook payload
	if receivedPayload.Command != "echo test output" {
		t.Errorf("Expected command 'echo test output', got %s", receivedPayload.Command)
	}

	// Webhook payload should not include webhook status fields
	if receivedPayload.WebhookSent {
		t.Error("Webhook payload should not include WebhookSent field")
	}

	// Verify output file was created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Output file was not created")
	}

	// Reset variables for next test
	resetWebhookGlobals()
}

func TestRunCommand_WithWebhookAuth(t *testing.T) {
	resetWebhookGlobals()
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	outputFile := filepath.Join(tmpDir, "output.txt")
	stderrFile := filepath.Join(tmpDir, "stderr.txt")

	if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		authType       string
		authToken      string
		expectedHeader string
		expectedValue  string
	}{
		{
			name:           "bearer auth",
			authType:       "bearer",
			authToken:      "test-bearer-token",
			expectedHeader: "Authorization",
			expectedValue:  "Bearer test-bearer-token",
		},
		{
			name:           "api-key auth",
			authType:       "api-key",
			authToken:      "test-api-key",
			expectedHeader: "X-API-Key",
			expectedValue:  "test-api-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global variables
			resetWebhookGlobals()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value := r.Header.Get(tt.expectedHeader)
				if value != tt.expectedValue {
					t.Errorf("Expected %s header to be '%s', got '%s'",
						tt.expectedHeader, tt.expectedValue, value)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			oldStdout := os.Stdout
			defer func() { os.Stdout = oldStdout }()

			r, w, _ := os.Pipe()
			os.Stdout = w

			rootCmd := &cobra.Command{}
			rootCmd.AddCommand(runCmd)

			args := []string{
				"run",
				"-i", inputFile,
				"-o", outputFile,
				"-e", stderrFile,
				"--webhook-url", server.URL,
				"--webhook-auth-type", tt.authType,
				"--webhook-auth-token", tt.authToken,
				"--webhook-retries", "0",
				"--",
				"true",
			}

			rootCmd.SetArgs(args)

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("Command failed: %v", err)
			}

			_ = w.Close()
			_, _ = io.Copy(io.Discard, r)
		})
	}
}

func TestRunCommand_WebhookRetry(t *testing.T) {
	resetWebhookGlobals()
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	outputFile := filepath.Join(tmpDir, "output.txt")
	stderrFile := filepath.Join(tmpDir, "stderr.txt")

	if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count <= 2 {
			// Fail first two attempts
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			// Succeed on third attempt
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(runCmd)

	args := []string{
		"run",
		"-i", inputFile,
		"-o", outputFile,
		"-e", stderrFile,
		"--webhook-url", server.URL,
		"--webhook-retries", "2",
		"--webhook-retry-delay", "10ms",
		"--",
		"true",
	}

	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Parse stdout JSON
	var result output.Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify webhook was sent after retries
	if !result.WebhookSent {
		t.Error("Expected webhook to be sent after retries")
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("Expected 3 attempts (initial + 2 retries), got %d", finalAttempts)
	}
}

func TestRunCommand_WebhookFailure(t *testing.T) {
	resetWebhookGlobals()
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	outputFile := filepath.Join(tmpDir, "output.txt")
	stderrFile := filepath.Join(tmpDir, "stderr.txt")

	if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Server always returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// Capture stdout and stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(runCmd)

	args := []string{
		"run",
		"-i", inputFile,
		"-o", outputFile,
		"-e", stderrFile,
		"--webhook-url", server.URL,
		"--webhook-retries", "0",
		"--",
		"true",
	}

	rootCmd.SetArgs(args)

	// Command should still succeed even if webhook fails
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Command should not fail due to webhook error: %v", err)
	}

	_ = wOut.Close()
	_ = wErr.Close()

	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)

	// Parse stdout JSON
	var result output.Result
	if err := json.Unmarshal(bufOut.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify webhook failed
	if result.WebhookSent {
		t.Error("Expected webhook_sent to be false")
	}

	if result.WebhookError == "" {
		t.Error("Expected webhook_error to be set")
	}

	// Verify error was logged to stderr
	stderrContent := bufErr.String()
	if !strings.Contains(stderrContent, "[WEBHOOK] Error:") {
		t.Error("Expected webhook error to be logged to stderr")
	}
}

func TestDiffCommand_WithWebhook(t *testing.T) {
	resetWebhookGlobals()
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "actual.txt")
	expectedFile := filepath.Join(tmpDir, "expected.txt")
	outputFile := filepath.Join(tmpDir, "diff.txt")
	stderrFile := filepath.Join(tmpDir, "stderr.txt")

	// Create test files
	if err := os.WriteFile(inputFile, []byte("Hello World\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expectedFile, []byte("Hello World\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var receivedPayload output.Result
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(diffCmd)

	args := []string{
		"diff",
		"-i", inputFile,
		"-x", expectedFile,
		"-o", outputFile,
		"-e", stderrFile,
		"--webhook-url", server.URL,
		"--webhook-retries", "0",
		"--score", "100",
	}

	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Parse stdout JSON
	var result output.Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify webhook was sent
	if !result.WebhookSent {
		t.Error("Expected webhook_sent to be true")
	}

	// Verify expected field in payload
	if receivedPayload.Expected == nil || *receivedPayload.Expected != expectedFile {
		t.Error("Expected field should be set in webhook payload for diff command")
	}

	// Verify score is included (files match, so score should be 100)
	if result.Score == nil || *result.Score != 100 {
		t.Error("Expected score to be 100 for matching files")
	}
}
