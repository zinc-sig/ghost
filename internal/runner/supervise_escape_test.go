//go:build linux

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// escapeGrower grows a shell string to 2^27 bytes, past any budget these
// tests set, holds it so a tick lands, and then marks done. Under a working
// budget the kill lands first and done never appears.
const escapeGrower = `a=x; i=0; while [ $i -lt 27 ]; do a="$a$a"; i=$((i+1)); done; sleep 3; touch "$1"`

// smallGrower allocates about 1 MiB, far under the budgets below.
const smallGrower = `a=x; i=0; while [ $i -lt 20 ]; do a="$a$a"; i=$((i+1)); done; sleep 1; touch "$1"`

// escapeScripts writes the grower and one launcher per escape pattern into
// dir and returns the launcher paths by name. Every launcher starts the
// grower somewhere outside the plain child position and then polls for the
// done file, because an orphan is not waitable by the original shell.
func escapeScripts(t *testing.T, dir, grower string) map[string]string {
	t.Helper()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	g := write("grow.sh", grower)
	done := filepath.Join(dir, "done")
	poll := fmt.Sprintf("\nwhile [ ! -f %s ]; do sleep 0.1; done", done)
	run := fmt.Sprintf("sh %s %s", g, done)
	py := fmt.Sprintf(`import os, sys
pid = os.fork()
if pid == 0:
    os.setsid()
    if os.fork() == 0:
        os.execvp("sh", ["sh", %q, %q])
    os._exit(0)
os.waitpid(pid, 0)
`, g, done)
	pyDaemon := write("daemon.py", py)
	pySetpgid := write("setpgid.py", fmt.Sprintf(`import os
os.setpgid(0, 0)
os.execvp("sh", ["sh", %q, %q])
`, g, done))
	return map[string]string{
		"setsid child":                 write("setsid-child.sh", fmt.Sprintf("setsid %s &", run)+poll),
		"setpgid child":                write("setpgid-child.sh", fmt.Sprintf("python3 %s &", pySetpgid)+poll),
		"double fork, same group":      write("double-same.sh", fmt.Sprintf("sh -c '%s &'", run)+poll),
		"double fork into new session": write("double-setsid.sh", fmt.Sprintf("setsid sh -c '%s &'", run)+poll),
		"triple fork into new session": write("triple.sh", fmt.Sprintf(`sh -c "setsid sh -c '%s &' &"`, run)+poll),
		"setsid inside setsid":         write("nested-setsid.sh", fmt.Sprintf(`setsid sh -c "setsid sh -c '%s &'" &`, run)+poll),
		"python daemonize":             write("daemonize.sh", fmt.Sprintf("python3 %s", pyDaemon)+poll),
	}
}

func needTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

// TestSuperviseMemoryBudgetEscapeMatrix asserts every way of leaving the
// command's position that seccomp allows (setsid, setpgid, reparenting by
// a double or triple fork, and their combinations) stays charged to the
// budget under supervise, which is a subreaper serving one exec: each
// escapee is killed past the budget and the trailer flags it.
func TestSuperviseMemoryBudgetEscapeMatrix(t *testing.T) {
	needTools(t, "setsid", "python3")
	dir := t.TempDir()
	for name, script := range escapeScripts(t, dir, escapeGrower) {
		t.Run(name, func(t *testing.T) {
			_ = os.Remove(filepath.Join(dir, "done"))
			sub := t.TempDir()
			cfg := superviseConfig(sub, "sh", script)
			cfg.MaxMemoryBytes = 32 << 20
			cfg.Timeout = 30 * time.Second
			if err := Supervise(cfg); err != nil {
				t.Fatalf("Supervise: %v", err)
			}
			tr := decodeResultFile(t, cfg.ResultFile)
			if !tr.MemoryLimitExceeded {
				t.Fatalf("escape not charged: memory_limit_exceeded=false exit=%d", tr.ExitCode)
			}
			if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
				t.Error("the grower finished: it was not killed")
			}
		})
	}
}

// TestSuperviseMemoryBudgetEscapeUnderBudgetPasses asserts the wider
// attribution raises no false kill: the same escape patterns with a grower
// far under the budget finish and pass unflagged with exit 0.
func TestSuperviseMemoryBudgetEscapeUnderBudgetPasses(t *testing.T) {
	needTools(t, "setsid", "python3")
	dir := t.TempDir()
	for name, script := range escapeScripts(t, dir, smallGrower) {
		t.Run(name, func(t *testing.T) {
			_ = os.Remove(filepath.Join(dir, "done"))
			sub := t.TempDir()
			cfg := superviseConfig(sub, "sh", script)
			cfg.MaxMemoryBytes = 64 << 20
			cfg.Timeout = 30 * time.Second
			if err := Supervise(cfg); err != nil {
				t.Fatalf("Supervise: %v", err)
			}
			tr := decodeResultFile(t, cfg.ResultFile)
			if tr.MemoryLimitExceeded || tr.ExitCode != 0 {
				t.Fatalf("under-budget escape flagged: memory_limit_exceeded=%v exit=%d", tr.MemoryLimitExceeded, tr.ExitCode)
			}
		})
	}
}

// TestSuperviseMemoryBudgetSweepsPidHopper asserts a process that keeps
// re-forking itself and exiting (so its pid changes faster than one sweep
// pass) does not outlive supervise: the repeated final sweep ends it.
func TestSuperviseMemoryBudgetSweepsPidHopper(t *testing.T) {
	needTools(t, "setsid", "pgrep")
	dir := t.TempDir()
	marker := fmt.Sprintf("ghost-hop-%d", time.Now().UnixNano())
	hop := filepath.Join(dir, "hop.sh")
	body := fmt.Sprintf("# %s\nsleep 0.05; setsid sh %s &\nexit 0\n", marker, hop)
	if err := os.WriteFile(hop, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("pkill", "-KILL", "-f", marker).Run() })
	cfg := superviseConfig(dir, "sh", "-c", fmt.Sprintf("setsid sh %s & sleep 1", hop))
	cfg.MaxMemoryBytes = 64 << 20
	cfg.Timeout = 30 * time.Second
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	out, _ := exec.Command("pgrep", "-f", hop).Output()
	if left := strings.TrimSpace(string(out)); left != "" {
		t.Fatalf("pid hopper survived supervise: %s", left)
	}
}

// TestSuperviseMemoryBudgetReapsOrphans asserts the subreaper reaps the
// orphans it inherits while the command runs, since a zombie counts against
// the task cap until it is reaped: after a stream of short-lived orphans the
// command sees at most a handful of zombie children of supervise, not one
// per orphan.
func TestSuperviseMemoryBudgetReapsOrphans(t *testing.T) {
	needTools(t, "setsid", "ps")
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c",
		`i=0; while [ $i -lt 100 ]; do setsid sh -c 'true &'; i=$((i+1)); sleep 0.02; done
sleep 0.3; ps -o stat= --ppid $PPID | grep -c Z`)
	cfg.MaxMemoryBytes = 64 << 20
	cfg.Timeout = 60 * time.Second
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	out, err := os.ReadFile(cfg.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	var zombies int
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &zombies); err != nil {
		t.Fatalf("zombie count %q: %v", out, err)
	}
	if zombies > 5 {
		t.Fatalf("%d zombie children of supervise after 100 orphans; orphans are not reaped", zombies)
	}
}

// TestSuperviseMemoryBudgetReapsOrphanBurst asserts orphans are reaped as
// they exit rather than on the next tick: right after a burst of 300
// orphans with no pause, the command sees at most a handful of zombie
// children of supervise. A tick-only reaper leaves dozens, which under a
// task cap of about a hundred makes the command's own forks fail.
func TestSuperviseMemoryBudgetReapsOrphanBurst(t *testing.T) {
	needTools(t, "ps")
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c",
		`i=0; while [ $i -lt 100 ]; do sh -c 'true & true & true &'; i=$((i+1)); done
sleep 0.02; ps -o stat= --ppid $PPID | grep -c Z`)
	cfg.MaxMemoryBytes = 64 << 20
	cfg.Timeout = 60 * time.Second
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	out, err := os.ReadFile(cfg.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	var zombies int
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &zombies); err != nil {
		t.Fatalf("zombie count %q: %v", out, err)
	}
	if zombies > 10 {
		t.Fatalf("%d zombie children of supervise right after a burst of 300 orphans; orphans are not reaped as they exit", zombies)
	}
}

// TestSuperviseMemoryBudgetKeepsExitCode asserts the subreaper's orphan
// reaping never steals the command's own exit status (it reaps by exact
// pid, never the command): a command that leaves orphans behind and exits
// 3 reports 3.
func TestSuperviseMemoryBudgetKeepsExitCode(t *testing.T) {
	needTools(t, "setsid")
	dir := t.TempDir()
	cfg := superviseConfig(dir, "sh", "-c",
		"for i in 1 2 3 4 5 6 7 8; do setsid sh -c 'sleep 0.2 &'; done; sleep 0.5; exit 3")
	cfg.MaxMemoryBytes = 64 << 20
	if err := Supervise(cfg); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	if tr := decodeResultFile(t, cfg.ResultFile); tr.ExitCode != 3 || tr.MemoryLimitExceeded {
		t.Fatalf("exit_code=%d memory_limit_exceeded=%v, want 3 and false", tr.ExitCode, tr.MemoryLimitExceeded)
	}
}
