//go:build linux

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func dumpable(t *testing.T) int {
	t.Helper()
	v, _, errno := unix.RawSyscall(unix.SYS_PRCTL, unix.PR_GET_DUMPABLE, 0, 0)
	if errno != 0 {
		t.Fatalf("PR_GET_DUMPABLE: %v", errno)
	}
	return int(v)
}

// TestDisableDumpable asserts that disableDumpable clears the flag for the
// calling process, that a child spawned afterwards is dumpable again after
// execve so the sampler can still read its smaps_rollup, and that a same-UID
// process cannot read the non-dumpable parent's environ. A regression in the
// first or third exposes the agent's credentials to student code; one in the
// second blinds the memory sampler.
func TestDisableDumpable(t *testing.T) {
	if err := disableDumpable(); err != nil {
		t.Fatalf("disableDumpable: %v", err)
	}
	t.Cleanup(func() { _ = unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0) })
	if got := dumpable(t); got != 0 {
		t.Fatalf("dumpable = %d after disableDumpable, want 0", got)
	}
	// The agent resolves its own binary through /proc/self/exe to spawn ghost
	// exec; the kernel exempts a process from the gate on its own entry.
	if _, err := os.Executable(); err != nil {
		t.Fatalf("os.Executable from the non-dumpable process: %v", err)
	}

	child := exec.Command("sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() })
	rollup := filepath.Join("/proc", strconv.Itoa(child.Process.Pid), "smaps_rollup")
	if _, err := os.ReadFile(rollup); err != nil {
		t.Errorf("read %s from the non-dumpable parent: %v (execve must reset the child's flag)", rollup, err)
	}

	if os.Geteuid() == 0 {
		t.Skip("root reads every /proc entry; the environ denial is observable only as a non-root user")
	}
	environ := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "environ")
	probe := exec.Command("cat", environ)
	if out, err := probe.CombinedOutput(); err == nil {
		t.Errorf("same-UID child read %s (%d bytes); the non-dumpable agent's environ must be unreadable", environ, len(out))
	}
}
