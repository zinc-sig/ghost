package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestApplySandboxEmptyWorkDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sandbox is a no-op on non-Linux")
	}

	err := ApplySandbox("")
	if err == nil {
		t.Fatal("expected error for empty workDir, got nil")
	}
}

const (
	applySandboxChildEnv = "GHOST_APPLY_SANDBOX_TEST_CHILD"
	applySandboxDirEnv   = "GHOST_APPLY_SANDBOX_TEST_DIR"
)

// TestApplySandboxValidWorkDir applies the sandbox in a forked child. Landlock
// cannot be lifted once applied, so applying it in the test process would
// confine every later test in this package to the temp dir.
func TestApplySandboxValidWorkDir(t *testing.T) {
	if os.Getenv(applySandboxChildEnv) == "1" {
		// CHILD: on Linux without Landlock support (or in unprivileged
		// environments), BestEffort degrades gracefully. We accept nil
		// (Landlock applied or best-effort no-op) or an error (kernel too
		// old, etc.). The test verifies no panic, which exits non-zero.
		_ = ApplySandbox(os.Getenv(applySandboxDirEnv))
		os.Exit(0)
	}

	// PARENT: fork the child with a temp work dir.
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplySandboxValidWorkDir$")
	cmd.Env = append(os.Environ(), applySandboxChildEnv+"=1", applySandboxDirEnv+"="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ApplySandbox child did not exit 0: %v\nchild output:\n%s", err, out)
	}
}
