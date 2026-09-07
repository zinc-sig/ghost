package agent

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// These tests run the real ghost binary (TestMain), so the child carries
// --oom-victim and the sampler measures a real process group. python3
// is the allocating child because it fills memory with one expression and
// can fork itself; the tests skip where it is absent.

func requirePython(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found; the memory budget tests need a child that can fill and fork")
	}
	return path
}

func memoryExec(python, script string, budget int64, prefix string) contract.RunExecInput {
	return contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:          python,
			Args:             []string{"-c", script},
			MemoryLimitBytes: budget,
			Workdir:          ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: prefix},
	}
}

// TestRunExec_MemoryLimitKillsAllocatingChild asserts that a child whose
// resident memory passes its budget is killed by the sampler and that the
// result carries the flag, the echoed budget, a measured peak, a signal
// exit, and no error. A kill reported as an error would turn a student
// fault into an infrastructure failure.
func TestRunExec_MemoryLimitKillsAllocatingChild(t *testing.T) {
	python := requirePython(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const budget = 16 << 20
	script := "import time; b = b'\\x01' * (64 << 20); time.sleep(5)"
	res := execRun(t, env, memoryExec(python, script, budget, "55/test/memkill"))

	if !res.MemoryLimitExceeded {
		t.Fatalf("MemoryLimitExceeded = false, want true (exit %s, peak %d, error %q)", fmtExitCode(res.ExitCode), res.PeakMemoryBytes, res.Error)
	}
	if res.MemoryLimitBytes != budget {
		t.Errorf("MemoryLimitBytes = %d, want %d", res.MemoryLimitBytes, budget)
	}
	if res.PeakMemoryBytes <= 0 {
		t.Errorf("PeakMemoryBytes = %d, want > 0", res.PeakMemoryBytes)
	}
	if res.ExitCode == nil || *res.ExitCode != -1 {
		t.Errorf("ExitCode = %s, want -1 (signal death)", fmtExitCode(res.ExitCode))
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want the empty string: a memory kill is a result", res.Error)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false (killed by the budget, not the deadline)")
	}
}

// TestRunExec_MemoryWithinBudgetCompletesWithPeak asserts that a child
// under a generous budget runs to completion unflagged and still reports a
// measured peak, so the sampler is shown to have observed the group.
func TestRunExec_MemoryWithinBudgetCompletesWithPeak(t *testing.T) {
	python := requirePython(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const budget = 256 << 20
	script := "import time; b = b'\\x01' * (32 << 20); time.sleep(0.6)"
	res := execRun(t, env, memoryExec(python, script, budget, "55/test/memok"))

	if res.MemoryLimitExceeded {
		t.Fatal("MemoryLimitExceeded = true, want false under a generous budget")
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %s, want 0 (error %q)", fmtExitCode(res.ExitCode), res.Error)
	}
	if res.PeakMemoryBytes < 32<<20 {
		t.Errorf("PeakMemoryBytes = %d, want at least the 32 MiB the child filled", res.PeakMemoryBytes)
	}
	if res.MemoryLimitBytes != budget {
		t.Errorf("MemoryLimitBytes = %d, want %d (echoed)", res.MemoryLimitBytes, budget)
	}
}

// TestRunExec_MemoryForkOvercountIsNotKilled asserts that a parent and its
// forked copy, whose RssAnon sum passes the budget while the pages are
// shared copy-on-write, are not killed: the Pss confirmation must reject
// the overcount. The peak assertion proves the overcount was observed, so
// a sampler that never saw both processes would fail here too.
func TestRunExec_MemoryForkOvercountIsNotKilled(t *testing.T) {
	python := requirePython(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const budget = 64 << 20
	script := strings.Join([]string{
		"import os, time",
		"b = b'\\x01' * (40 << 20)",
		"pid = os.fork()",
		"if pid == 0:",
		"    time.sleep(1.5); os._exit(0)",
		"time.sleep(1.5); os.waitpid(pid, 0)",
	}, "\n")
	res := execRun(t, env, memoryExec(python, script, budget, "55/test/memfork"))

	if res.MemoryLimitExceeded {
		t.Fatalf("MemoryLimitExceeded = true, want false: the Pss confirmation must reject the copy-on-write overcount (peak %d)", res.PeakMemoryBytes)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %s, want 0 (error %q)", fmtExitCode(res.ExitCode), res.Error)
	}
	if res.PeakMemoryBytes <= budget {
		t.Errorf("PeakMemoryBytes = %d, want > %d: the resident sum of parent and child must have passed the budget for the confirmation to be exercised", res.PeakMemoryBytes, budget)
	}
}

// TestRunExec_MemoryBudgetZeroIsNotEnforced asserts that a budget of 0
// disables enforcement and echoes 0 while the peak is still measured. An
// agent-side default here would kill execs the container had room for.
func TestRunExec_MemoryBudgetZeroIsNotEnforced(t *testing.T) {
	python := requirePython(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	script := "import time; b = b'\\x01' * (64 << 20); time.sleep(0.6)"
	res := execRun(t, env, memoryExec(python, script, 0, "55/test/memzero"))

	if res.MemoryLimitExceeded {
		t.Fatal("MemoryLimitExceeded = true, want false with no budget")
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %s, want 0 (error %q)", fmtExitCode(res.ExitCode), res.Error)
	}
	if res.MemoryLimitBytes != 0 {
		t.Errorf("MemoryLimitBytes = %d, want 0", res.MemoryLimitBytes)
	}
	if res.PeakMemoryBytes <= 0 {
		t.Errorf("PeakMemoryBytes = %d, want > 0: the peak is measured even without a budget", res.PeakMemoryBytes)
	}
}

// TestRunExec_SweepKillsSetsidEscapee asserts that a process which left the
// exec's process group with setsid is killed by the sweep after the exec
// ends. The group kill cannot reach it, so a missing sweep would let it
// carry memory and CPU into later execs.
func TestRunExec_SweepKillsSetsidEscapee(t *testing.T) {
	python := requirePython(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	script := "import subprocess; p = subprocess.Popen(['sleep', '60'], start_new_session=True); print(p.pid)"
	res := execRun(t, env, memoryExec(python, script, 0, "55/test/setsid"))

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %s, want 0 (error %q)", fmtExitCode(res.ExitCode), res.Error)
	}
	data, ok := store.upload("runs", "55/test/setsid/stdout")
	if !ok {
		t.Fatal("stdout upload missing")
	}
	pid, err := strconv.Atoi(string(bytes.TrimSpace(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("could not parse the escapee pid from stdout %q: %v", data, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) || stragglerIsZombie(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("setsid escapee pid %d still alive 3s after the exec returned", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// containerMemoryCap returns the cgroup memory cap of the test process, or
// skips when the process is not in a capped cgroup v2 leaf with a readable
// oom_kill counter. The kernel path of the sampler can only be exercised
// under such a cap; the suite is run inside a memory-capped container for
// that.
func containerMemoryCap(t *testing.T) int64 {
	t.Helper()
	raw, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		t.Skip("no cgroup v2 memory.max at the cgroup root; run the suite inside a memory-capped container")
	}
	capBytes, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		t.Skip("memory.max is not a number (uncapped cgroup); run the suite inside a memory-capped container")
	}
	if _, err := os.ReadFile("/sys/fs/cgroup/memory.events"); err != nil {
		t.Skip("memory.events unreadable; the kernel path cannot be attributed here")
	}
	return capBytes
}

// TestRunExec_KernelKillAttributedToSoleExec asserts that a child whose
// budget is above the container cap, so the sampler never fires, is
// killed by the kernel at the cap and the kill is attributed to it as the
// only running exec: the flag is set, the exit is a signal death, and the
// agent itself survives to return the result. A sampler that only knew
// its own kills would report an unflagged crash here.
func TestRunExec_KernelKillAttributedToSoleExec(t *testing.T) {
	python := requirePython(t)
	capBytes := containerMemoryCap(t)
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	budget := 2 * capBytes
	// Fill in 8 MiB steps so the cap is crossed between ticks by a wide
	// margin only once, not in a single allocation the sampler could see
	// first.
	script := "import time\nb = bytearray()\nfor _ in range(" + strconv.FormatInt(budget/(8<<20), 10) + "):\n    b.extend(bytes(8 << 20))\ntime.sleep(5)"
	res := execRun(t, env, memoryExec(python, script, budget, "55/test/kernelkill"))

	if !res.MemoryLimitExceeded {
		t.Fatalf("MemoryLimitExceeded = false, want true (exit %s, peak %d, error %q)", fmtExitCode(res.ExitCode), res.PeakMemoryBytes, res.Error)
	}
	if res.ExitCode == nil || *res.ExitCode != -1 {
		t.Errorf("ExitCode = %s, want -1 (signal death)", fmtExitCode(res.ExitCode))
	}
	if res.PeakMemoryBytes <= 0 || res.PeakMemoryBytes > budget {
		t.Errorf("PeakMemoryBytes = %d, want a measured value under the %d budget", res.PeakMemoryBytes, budget)
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want the empty string: a kernel kill is a result", res.Error)
	}
}
