package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// These tests run the REAL ghost binary (TestMain), so they exercise the
// full enforcement chain: agent flag plumbing -> cmd/exec.go ->
// RLIMIT_FSIZE in the child -> SIGXFSZ / capture-size adjudication.

// TestRunExec_OutputLimitKillsFloodingChild: the direct child writes past
// the limit and dies on SIGXFSZ; the capture and the uploaded object stop
// at exactly the limit (core backend/12: the d57 grader-bomb wrote 997MB
// through this then-uncapped path).
func TestRunExec_OutputLimitKillsFloodingChild(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const limit = 64 * 1024
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:          "/bin/dd",
			Args:             []string{"if=/dev/zero", "bs=4096", "count=32"}, // 128 KiB attempted
			OutputLimitBytes: limit,
			Workdir:          ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/flood"},
	}
	res := execRun(t, env, input)

	if !res.OutputLimitExceeded {
		t.Fatalf("OutputLimitExceeded = false, want true (error: %q)", res.Error)
	}
	if res.OutputLimitBytes != limit {
		t.Errorf("OutputLimitBytes = %d, want %d", res.OutputLimitBytes, limit)
	}
	// SIGXFSZ death surfaces as a signal exit, matching os/exec convention.
	if res.ExitCode == nil || *res.ExitCode != -1 {
		t.Errorf("ExitCode = %v, want -1 (signal death)", res.ExitCode)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false (killed by the output limit, not the deadline)")
	}
	// RLIMIT_FSIZE permits growth exactly to the limit and refuses the
	// byte after it.
	if res.StdoutBytes != limit {
		t.Errorf("StdoutBytes = %d, want exactly %d", res.StdoutBytes, limit)
	}
	if data, ok := store.upload("runs", "55/test/flood/stdout"); !ok || int64(len(data)) != limit {
		t.Errorf("uploaded stdout = %d bytes (present=%v), want %d", len(data), ok, limit)
	}
}

// TestRunExec_OutputLimitWorkdirFloodDetectedBySignal: the direct child
// SIGXFSZ-dies flooding a WORKDIR file while both stdio captures stay
// under the limit — so the capture-size layer cannot fire and detection
// rests ENTIRELY on the signal branch (the case that layer uniquely
// covers; without it this run would read as a bare student crash).
func TestRunExec_OutputLimitWorkdirFloodDetectedBySignal(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const limit = 64 * 1024
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:          "/bin/dd",
			Args:             []string{"if=/dev/zero", "of=flood.bin", "bs=4096", "count=32"}, // 128 KiB attempted
			OutputLimitBytes: limit,
			Workdir:          ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/wdflood"},
	}
	res := execRun(t, env, input)

	if !res.OutputLimitExceeded {
		t.Fatalf("OutputLimitExceeded = false, want true via the SIGXFSZ signal branch (error: %q)", res.Error)
	}
	if res.ExitCode == nil || *res.ExitCode != -1 {
		t.Errorf("ExitCode = %v, want -1 (signal death)", res.ExitCode)
	}
	// The discriminating assertions: both captures are under the limit, so
	// the size layer provably did NOT produce the flag.
	if res.StdoutBytes >= limit || res.StderrBytes >= limit {
		t.Fatalf("captures not under the limit (stdout=%d stderr=%d) — test no longer isolates the signal branch", res.StdoutBytes, res.StderrBytes)
	}
	// The workdir file itself is still bounded by the rlimit.
	if st, err := os.Stat(filepath.Join(cfg.Workdir, "flood.bin")); err != nil || st.Size() != limit {
		t.Errorf("flood.bin stat = %v, %v; want size exactly %d", st, err, limit)
	}
}

// TestRunExec_OutputLimitGrandchildDetectedBySize: a DESCENDANT takes the
// SIGXFSZ while the direct child exits 0 — the capture-size layer must
// still flag the exec. This layer is load-bearing, not belt-and-braces.
func TestRunExec_OutputLimitGrandchildDetectedBySize(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	const limit = 64 * 1024
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:          "/bin/sh",
			Args:             []string{"-c", "dd if=/dev/zero bs=4096 count=32 2>/dev/null; exit 0"},
			OutputLimitBytes: limit,
			Workdir:          ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/gc"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (the shell survives its dd child's SIGXFSZ)", res.ExitCode)
	}
	if !res.OutputLimitExceeded {
		t.Fatal("OutputLimitExceeded = false, want true via the capture-size layer")
	}
	if res.StdoutBytes != limit {
		t.Errorf("StdoutBytes = %d, want exactly %d", res.StdoutBytes, limit)
	}
}

// TestRunExec_OutputLimitDefaultApplies: ExecSpec.OutputLimitBytes == 0
// falls back to the agent default (GHOST_AGENT_DEFAULT_OUTPUT_LIMIT in
// production), which must be enforced and echoed like a spec value.
func TestRunExec_OutputLimitDefaultApplies(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DefaultOutputLimit = 32 * 1024
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command: "/bin/dd",
			Args:    []string{"if=/dev/zero", "bs=4096", "count=16"}, // 64 KiB attempted
			Workdir: ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/def"},
	}
	res := execRun(t, env, input)

	if !res.OutputLimitExceeded {
		t.Fatalf("OutputLimitExceeded = false, want true under the agent default (error: %q)", res.Error)
	}
	if res.OutputLimitBytes != cfg.DefaultOutputLimit {
		t.Errorf("OutputLimitBytes = %d, want the default %d", res.OutputLimitBytes, cfg.DefaultOutputLimit)
	}
	if res.StdoutBytes != cfg.DefaultOutputLimit {
		t.Errorf("StdoutBytes = %d, want exactly %d", res.StdoutBytes, cfg.DefaultOutputLimit)
	}
}

// TestRunExec_OutputWithinLimitNotFlagged: a normal exec under the limit
// is untouched and still reports its capture sizes as grader metadata.
func TestRunExec_OutputWithinLimitNotFlagged(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:          "/bin/echo",
			Args:             []string{"hello"},
			OutputLimitBytes: 64 * 1024,
			Workdir:          ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/ok"},
	}
	res := execRun(t, env, input)

	if res.OutputLimitExceeded {
		t.Fatal("OutputLimitExceeded = true, want false for output within the limit")
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", res.ExitCode)
	}
	if want := int64(len("hello\n")); res.StdoutBytes != want {
		t.Errorf("StdoutBytes = %d, want %d", res.StdoutBytes, want)
	}
	if res.StderrBytes != 0 {
		t.Errorf("StderrBytes = %d, want 0", res.StderrBytes)
	}
	if res.OutputLimitBytes != 64*1024 {
		t.Errorf("OutputLimitBytes = %d, want %d (echoed)", res.OutputLimitBytes, 64*1024)
	}
}

// TestRunExec_NormalExitKillsStragglers: a descendant backgrounded past the
// direct child's exit must be SIGKILLed with the process group before the
// activity returns — previously only the timeout branch killed the group,
// so survivors could keep writing captures and linger into later execs.
func TestRunExec_NormalExitKillsStragglers(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30 & echo $!"},
			Workdir: ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/orphan"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	data, ok := store.upload("runs", "55/test/orphan/stdout")
	if !ok {
		t.Fatal("stdout upload missing")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(bytes.TrimSpace(data))))
	if err != nil || pid <= 0 {
		t.Fatalf("could not parse straggler pid from stdout %q: %v", data, err)
	}

	// The sleeper must be dead (ESRCH) or a zombie awaiting init's reap —
	// anything still running means the group was not killed.
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) || stragglerIsZombie(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("straggler pid %d still alive 3s after the exec returned", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stragglerIsZombie reports whether pid is a zombie (state Z in
// /proc/<pid>/stat) — killed, merely unreaped by its new parent yet.
func stragglerIsZombie(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	data := string(b)
	// state is the field after the parenthesised comm.
	if i := strings.LastIndexByte(data, ')'); i >= 0 && i+2 < len(data) {
		return data[i+2] == 'Z'
	}
	return false
}
