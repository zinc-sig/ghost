//go:build linux

package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/sandbox"
)

// decodeResultFile reads and JSON-decodes the supervise result file (the
// primary Docker transport — plain JSON, no frame sentinels).
func decodeResultFile(t *testing.T, path string) output.Trailer {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result file: %v", err)
	}
	var tr output.Trailer
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatalf("decode trailer: %v", err)
	}
	return tr
}

func superviseConfig(dir string, cmd string, args ...string) *Config {
	return &Config{
		Command:        cmd,
		Args:           args,
		InputFile:      "/dev/null",
		OutputFile:     filepath.Join(dir, "stdout"),
		StderrFile:     filepath.Join(dir, "stderr"),
		Supervise:      true,
		MaxOutputBytes: 1 << 20,
		ResultFile:     filepath.Join(dir, ".result"),
	}
}

func TestSuperviseHappyPath(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo hello; sleep 0.1")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}

	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.Schema != output.TrailerSchema {
		t.Errorf("schema = %d, want %d", tr.Schema, output.TrailerSchema)
	}
	if tr.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", tr.ExitCode)
	}
	if tr.OOMKilled {
		t.Error("oom_killed = true on happy path")
	}
	if tr.Truncated {
		t.Error("truncated = true on small output")
	}
	if tr.DurationMs < 90 {
		t.Errorf("duration_ms = %d, want >= ~100 (child sleeps 0.1s)", tr.DurationMs)
	}
	if sandbox.CgroupV2Available() && tr.PeakMemoryB <= 0 {
		t.Errorf("peak_memory_bytes = %d, want > 0 on a cgroup-v2 host", tr.PeakMemoryB)
	}
	assertFileContains(t, cfg.OutputFile, "hello\n")
}

// TestSuperviseFilePermissions locks the 0600 hardening: the result, stdout and
// stderr files must not be world/group-writable. The forge-proof channel is the
// stdout frame, but the at-rest artifacts should still be owner-only.
func TestSuperviseFilePermissions(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo hi; echo err 1>&2")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	for _, path := range []string{cfg.ResultFile, cfg.OutputFile, cfg.StderrFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %#o, want 0600", path, perm)
		}
	}
}

func TestSuperviseExitCode(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "exit 7")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 7 {
		t.Errorf("exit_code = %d, want 7", tr.ExitCode)
	}
}

func TestSuperviseCreatesResultFileDir(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "echo hi")
	// Point --result-file at a not-yet-existing nested directory; writeTrailer
	// must create it (like stdout/stderr) rather than failing with ENOENT.
	cfg.ResultFile = filepath.Join(dir, "nested", "sub", ".result")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", tr.ExitCode)
	}
}

func TestSuperviseTruncatesOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "head -c 100 /dev/zero | tr '\\0' 'a'")
	cfg.MaxOutputBytes = 10
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if !tr.Truncated {
		t.Error("truncated = false, want true (output exceeded cap)")
	}
	data, err := os.ReadFile(cfg.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > cfg.MaxOutputBytes {
		t.Errorf("output file %d bytes, want <= cap %d", len(data), cfg.MaxOutputBytes)
	}
}

func TestResolvePeak(t *testing.T) {
	tests := []struct {
		name      string
		baseline  int64
		watermark int64
		sampled   int64
		want      int64
	}{
		{"watermark above baseline wins", 100, 150, 120, 150},
		{"watermark equal to baseline falls back to sampled", 100, 100, 120, 120},
		{"watermark below baseline falls back to sampled", 100, 90, 130, 130},
		{"zero watermark falls back to sampled", 0, 0, 64, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePeak(tt.baseline, tt.watermark, tt.sampled); got != tt.want {
				t.Errorf("resolvePeak(%d, %d, %d) = %d, want %d",
					tt.baseline, tt.watermark, tt.sampled, got, tt.want)
			}
		})
	}
}

// runForExitErr runs a small command and returns the wait error so the test can
// exercise exitCodeFor against a real *exec.ExitError / syscall.WaitStatus.
func runForExitErr(t *testing.T, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func TestExitCodeFor(t *testing.T) {
	// timedOut short-circuits to -1 regardless of waitErr.
	if got := exitCodeFor(nil, true); got != -1 {
		t.Errorf("exitCodeFor(nil, timedOut=true) = %d, want -1", got)
	}
	if got := exitCodeFor(errors.New("anything"), true); got != -1 {
		t.Errorf("exitCodeFor(err, timedOut=true) = %d, want -1", got)
	}

	// Normal nil error => 0.
	if got := exitCodeFor(nil, false); got != 0 {
		t.Errorf("exitCodeFor(nil, false) = %d, want 0", got)
	}

	// Normal non-zero exit => the wait-status exit code is preserved.
	err := runForExitErr(t, "sh", "-c", "exit 7")
	if got := exitCodeFor(err, false); got != 7 {
		t.Errorf("exitCodeFor(exit-7, false) = %d, want 7", got)
	}

	// Signalled-but-not-timeout (SIGSEGV) => -1.
	err = runForExitErr(t, "sh", "-c", "kill -SEGV $$")
	if got := exitCodeFor(err, false); got != -1 {
		t.Errorf("exitCodeFor(SIGSEGV, false) = %d, want -1", got)
	}

	// Non-*exec.ExitError wait failure => -1 (abnormal).
	if got := exitCodeFor(fmt.Errorf("wait failed: %w", os.ErrClosed), false); got != -1 {
		t.Errorf("exitCodeFor(non-ExitError, false) = %d, want -1", got)
	}
}

func TestSuperviseTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sleep", "5")
	cfg.Timeout = 200 * time.Millisecond
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != -1 {
		t.Errorf("exit_code = %d, want -1 on timeout", tr.ExitCode)
	}
}

// TestWriteTrailer_TightensPreexistingResultFile guards the file-perms fix for
// the supervise result file: writeTrailer now open+fchmods rather than
// os.WriteFile, so a PRE-EXISTING broader-mode result file (owned by this
// process) is tightened to 0600 and the trailer is still written correctly.
func TestWriteTrailer_TightensPreexistingResultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, []byte("stale"), 0o666); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("pre-chmod: %v", err)
	}
	want := output.Trailer{Schema: 1, ExitCode: 7, DurationMs: 42}
	if err := writeTrailer(path, want); err != nil {
		t.Fatalf("writeTrailer: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o after tightening a pre-existing 0666 result file, want 0600", perm)
	}
	if got := decodeResultFile(t, path); got != want {
		t.Errorf("trailer = %+v, want %+v", got, want)
	}
}
