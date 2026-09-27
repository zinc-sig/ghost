//go:build linux

package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// holderGone polls until the pid recorded in pidFile no longer runs "sleep",
// or the deadline passes.
func holderGone(t *testing.T, pidFile string) bool {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("holder pid file: %v", err)
	}
	pid := strings.TrimSpace(string(raw))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cmdline, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		if err != nil || !strings.HasPrefix(string(cmdline), "sleep") {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// TestSuperviseHolderEndsWithCommand asserts a process the command leaves
// behind holding the stdout pipe does not stall supervise: the run ends
// with the command's own exit status shortly after the command exits, and
// the leftover is killed. No timeout is configured, matching how core runs
// supervise, and the kill is membership-based rather than budget-based, so
// the cases cover a plain background child and a setsid one, with and
// without a budget.
func TestSuperviseHolderEndsWithCommand(t *testing.T) {
	needTools(t, "setsid")
	cases := []struct {
		name   string
		script string
		budget int64
		exit   int
	}{
		{"background holder with budget", "echo out; sleep 30 & echo $! > %s; exit 0", 64 << 20, 0},
		{"background holder without budget", "echo out; sleep 30 & echo $! > %s; exit 0", 0, 0},
		{"setsid holder without budget", "echo out; setsid sh -c 'sleep 30 & echo $! > %s'; exit 0", 0, 0},
		{"background holder keeps exit code", "echo out; sleep 30 & echo $! > %s; exit 3", 64 << 20, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "holder-pid")
			cfg := superviseConfig(dir, "sh", "-c", fmt.Sprintf(c.script, pidFile))
			cfg.MaxMemoryBytes = c.budget
			start := time.Now()
			if err := Supervise(cfg); err != nil {
				t.Fatalf("Supervise: %v", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("supervise stalled %v behind the leftover pipe holder", elapsed)
			}
			tr := decodeResultFile(t, cfg.ResultFile)
			if tr.ExitCode != c.exit || tr.MemoryLimitExceeded {
				t.Fatalf("exit_code=%d memory_limit_exceeded=%v, want %d and false", tr.ExitCode, tr.MemoryLimitExceeded, c.exit)
			}
			out, err := os.ReadFile(cfg.OutputFile)
			if err != nil || !strings.Contains(string(out), "out") {
				t.Fatalf("stdout %q (%v), want the command's output captured", out, err)
			}
			if !holderGone(t, pidFile) {
				t.Fatal("the leftover pipe holder outlived supervise")
			}
		})
	}
}
