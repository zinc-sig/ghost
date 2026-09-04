//go:build linux

package runner

import (
	"os"
	"testing"

	"github.com/zinc-sig/ghost/internal/sandbox"
)

// requireCgroupV2Runner skips a supervise test that asserts on cgroup-derived
// fields when cgroup v2 memory accounting is not readable at the cgroupns root.
func requireCgroupV2Runner(t *testing.T) {
	t.Helper()
	if !sandbox.CgroupV2Available() {
		t.Skip("cgroup v2 memory accounting not available at cgroupns root")
	}
}

// TestSupervisePeakMemorySampled asserts the supervisor records a non-zero peak
// for a child that allocates memory, exercising the sampling + baseline-delta
// path end to end. Gated on a cgroup-v2 host.
func TestSupervisePeakMemorySampled(t *testing.T) {
	requireCgroupV2Runner(t)
	dir := t.TempDir()
	// Allocate ~32 MiB in the shell and hold it briefly so the 25ms sampler
	// observes the spike.
	script := `a=$(head -c 33554432 /dev/zero | tr '\0' 'x'); sleep 0.2; printf '%s' "${#a}"`
	cfg := superviseConfig(dir, "sh", "-c", script)
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", tr.ExitCode)
	}
	if tr.PeakMemoryB <= 0 {
		t.Errorf("peak_memory_bytes = %d, want > 0 for a memory-allocating child", tr.PeakMemoryB)
	}
	if tr.OOMKilled {
		t.Error("oom_killed = true, want false on the happy path")
	}
}

// TestSuperviseSandboxedPeakMemory is the regression guard for the Landlock
// blocker: with Landlock:true, Landlock is applied before the supervisor's
// cgroup reads, so omitting /sys/fs/cgroup from the allowlist makes those reads
// EACCES and peak_memory_bytes always 0. It runs a memory-allocating child
// under the full sandbox and asserts a non-zero peak.
//
// Gated to SKIP unless BOTH Landlock (ABI>=1, so BestEffort actually enforces)
// AND cgroup v2 are available at the cgroupns root. On hosts missing either,
// the bug is unobservable, so the test would not catch a regression. It skips on
// dev/CI hosts without Landlock and enforces the contract on capable hosts.
func TestSuperviseSandboxedPeakMemory(t *testing.T) {
	requireCgroupV2Runner(t)
	if !sandbox.LandlockAvailable() {
		t.Skip("Landlock not available (ABI < 1): BestEffort no-ops, so the cgroup-under-Landlock interaction is not exercised")
	}
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c",
		`a=$(head -c 33554432 /dev/zero | tr '\0' 'x'); sleep 0.2; printf '%s' "${#a}"`)
	cfg.Landlock = true
	cfg.SandboxWorkDir = dir
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if tr.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", tr.ExitCode)
	}
	if tr.PeakMemoryB <= 0 {
		t.Errorf("peak_memory_bytes = %d, want > 0 for a memory-allocating sandboxed child "+
			"(Landlock must allow /sys/fs/cgroup reads)", tr.PeakMemoryB)
	}
}

// TestSuperviseOOMOptIn exercises the OOM-attribution path. Forcing an OOM
// portably is hard (needs a tight memory cgroup), so it is opt-in via
// GHOST_TEST_OOM; otherwise the happy-path assertion stands.
func TestSuperviseOOMOptIn(t *testing.T) {
	requireCgroupV2Runner(t)
	if os.Getenv("GHOST_TEST_OOM") == "" {
		t.Skip("set GHOST_TEST_OOM=1 with a tight memory cgroup to exercise OOM attribution")
	}
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c", "a=$(head -c 1073741824 /dev/zero | tr '\\0' 'x'); printf '%s' \"${#a}\"")
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	tr := decodeResultFile(t, cfg.ResultFile)
	if !tr.OOMKilled {
		t.Errorf("oom_killed = false, want true under a tight memory cgroup")
	}
	if tr.ExitCode != -1 {
		t.Errorf("exit_code = %d, want -1 for an OOM-killed child", tr.ExitCode)
	}
}
