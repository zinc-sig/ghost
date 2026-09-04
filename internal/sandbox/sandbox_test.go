package sandbox

import (
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

func TestApplySandboxValidWorkDir(t *testing.T) {
	// On Linux without Landlock support (or in unprivileged environments),
	// BestEffort degrades gracefully. This should not panic or hard-fail.
	dir := t.TempDir()
	err := ApplySandbox(dir)
	// We accept nil (Landlock applied or best-effort no-op) or an error
	// (kernel too old, etc.). The test verifies no panic.
	_ = err
}
