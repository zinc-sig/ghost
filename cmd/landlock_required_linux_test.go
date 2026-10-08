//go:build linux

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zinc-sig/ghost/internal/sandbox"
)

// denyLandlockProfileJSON allows every syscall except the Landlock ones, so
// a process under it sees a kernel without Landlock. Container runtimes
// whose seccomp profile predates Landlock present ghost with this view.
const denyLandlockProfileJSON = `{
  "defaultAction": "SCMP_ACT_ALLOW",
  "architectures": ["SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"],
  "syscalls": [
    {"names": ["landlock_create_ruleset", "landlock_add_rule", "landlock_restrict_self"], "action": "SCMP_ACT_ERRNO"}
  ]
}`

// runWithoutLandlock runs `ghost <args...>` under denyLandlockProfileJSON:
// an outer `ghost exec` installs the profile and replaces itself with the
// inner ghost, which inherits the filter. The inner ghost's stderr is the
// outer capture file, returned with the exit error.
func runWithoutLandlock(t *testing.T, bin string, env []string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	stderr := filepath.Join(dir, "outer-stderr")
	cmd := exec.Command(bin, append([]string{
		"exec",
		"--seccomp-profile-json=" + denyLandlockProfileJSON,
		"-i", "/dev/null",
		"-o", filepath.Join(dir, "outer-stdout"),
		"-e", stderr,
		"--", bin,
	}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	runErr := cmd.Run()
	out, err := os.ReadFile(stderr)
	if err != nil {
		t.Fatalf("read inner stderr: %v", err)
	}
	return string(out), runErr
}

// TestLandlockRequired_SandboxedCommandsRefuse asserts that `exec
// --landlock` and `supervise --landlock` refuse to run the command when
// Landlock is unavailable, report why on ghost's own stderr, and leave no
// capture behind; and that GHOST_LANDLOCK_BEST_EFFORT=true runs the command
// without Landlock. A silent fallback would run the command with no
// filesystem boundary.
func TestLandlockRequired_SandboxedCommandsRefuse(t *testing.T) {
	bin := buildGhostBin(t)

	for _, mode := range []string{"exec", "supervise"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			capture := filepath.Join(work, "stdout")
			args := []string{mode, "--landlock", "--workdir", work,
				"-i", "/dev/null", "-o", capture, "-e", filepath.Join(work, "stderr")}
			if mode == "supervise" {
				args = append(args, "--result-file="+filepath.Join(work, ".result"))
			}
			args = append(args, "--", "/bin/echo", "ran")

			stderr, err := runWithoutLandlock(t, bin, []string{sandbox.EnvLandlockBestEffort + "="}, args...)
			if err == nil {
				t.Fatalf("ghost %s ran the command without Landlock; stderr:\n%s", mode, stderr)
			}
			if !strings.Contains(stderr, "Landlock is unavailable") || !strings.Contains(stderr, sandbox.EnvLandlockBestEffort) {
				t.Errorf("stderr = %q, want the Landlock refusal naming %s", stderr, sandbox.EnvLandlockBestEffort)
			}
			if _, err := os.Stat(capture); err == nil {
				t.Errorf("ghost %s created the capture %s for a command it refused to run", mode, capture)
			}
		})
	}

	t.Run("best effort runs without Landlock", func(t *testing.T) {
		work := t.TempDir()
		capture := filepath.Join(work, "stdout")
		stderr, err := runWithoutLandlock(t, bin, []string{sandbox.EnvLandlockBestEffort + "=true"},
			"exec", "--landlock", "--workdir", work,
			"-i", "/dev/null", "-o", capture, "-e", filepath.Join(work, "stderr"),
			"--", "/bin/echo", "ran")
		if err != nil {
			t.Fatalf("ghost exec with %s=true failed: %v; stderr:\n%s", sandbox.EnvLandlockBestEffort, err, stderr)
		}
		if data, _ := os.ReadFile(capture); string(data) != "ran\n" {
			t.Errorf("capture = %q, want %q", data, "ran\n")
		}
	})
}

// TestLandlockRequired_AgentRefusesToStart asserts that the agent exits at
// startup with the Landlock refusal when its sandbox is on and Landlock is
// unavailable. Each exec child would otherwise refuse with exit status 1,
// which the result reports as the command's own failure.
func TestLandlockRequired_AgentRefusesToStart(t *testing.T) {
	bin := buildGhostBin(t)
	env := []string{
		sandbox.EnvLandlockBestEffort + "=",
		"GHOST_AGENT_SANDBOX=true",
		"GHOST_AGENT_TEMPORAL_ADDRESS=127.0.0.1:1",
		"GHOST_AGENT_TASK_QUEUE=landlock-required",
		"GHOST_AGENT_STORAGE_ENDPOINT=127.0.0.1:1",
		"GHOST_AGENT_STORAGE_ACCESS_KEY=test",
		"GHOST_AGENT_STORAGE_SECRET_KEY=test",
		"GHOST_AGENT_WORKDIR=" + t.TempDir(),
		"GHOST_AGENT_STAGING_DIR=" + t.TempDir(),
	}

	stderr, err := runWithoutLandlock(t, bin, env, "agent")
	if err == nil {
		t.Fatalf("the agent started without Landlock; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "agent: sandbox: Landlock is unavailable") {
		t.Errorf("stderr = %q, want the agent's Landlock refusal", stderr)
	}
	if strings.Contains(stderr, "temporal") && strings.Contains(stderr, "failed to connect") {
		t.Errorf("the agent tried to join the queue before refusing: %q", stderr)
	}
}
