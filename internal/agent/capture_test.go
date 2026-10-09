package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// captureRun runs one command whose stdout is copied to stdoutPath and
// returns the result. The command stands in for a student program that
// prepares the workspace before the agent copies the capture.
func captureRun(t *testing.T, cfg *Config, stdoutPath string, command string, args ...string) contract.ExecResult {
	t.Helper()
	env := newActivityEnv(t, cfg, newFakeStore())
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:    command,
			Args:       args,
			Workdir:    ".",
			StdoutPath: &stdoutPath,
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/capture"},
	}
	return execRun(t, env, input)
}

// TestRunExec_StdoutPathSymlinkOutsideWorkspaceNotFollowed asserts that a
// symlink the command leaves at stdout_path is not followed: the file it
// points at outside the workspace is not created. Following it lets the
// sandboxed command write chosen bytes anywhere the unsandboxed agent can.
func TestRunExec_StdoutPathSymlinkOutsideWorkspaceNotFollowed(t *testing.T) {
	cfg := newTestConfig(t)
	outside := filepath.Join(t.TempDir(), "outside-target")

	res := captureRun(t, cfg, "answer.txt", "/bin/ln", "-s", outside, "answer.txt")

	if _, err := os.Lstat(outside); err == nil {
		t.Fatalf("the agent wrote the capture through the symlink to %s", outside)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Errorf("ExitCode = %s, want 0: the command itself succeeded", fmtExitCode(res.ExitCode))
	}
	if !strings.Contains(res.Error, "stdout_path") {
		t.Errorf("Error = %q, want the refused stdout_path copy reported", res.Error)
	}
}

// TestRunExec_StdoutPathSymlinkInsideWorkspaceNotFollowed asserts that a
// symlink pointing at another workspace file is refused as well, so the
// capture cannot overwrite a file the pipeline placed for a later stage.
func TestRunExec_StdoutPathSymlinkInsideWorkspaceNotFollowed(t *testing.T) {
	cfg := newTestConfig(t)
	expected := filepath.Join(cfg.Workdir, "expected.txt")
	if err := os.WriteFile(expected, []byte("teacher\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := captureRun(t, cfg, "answer.txt", "/bin/ln", "-s", "expected.txt", "answer.txt")

	if data, err := os.ReadFile(expected); err != nil || string(data) != "teacher\n" {
		t.Errorf("expected.txt = %q (err %v), want it untouched", data, err)
	}
	if !strings.Contains(res.Error, "stdout_path") {
		t.Errorf("Error = %q, want the refused stdout_path copy reported", res.Error)
	}
}

// TestRunExec_StdoutPathSymlinkedParentNotFollowed asserts that a symlink
// in a parent directory of stdout_path is not followed either; checking
// the final component alone would let a symlinked directory redirect the
// write.
func TestRunExec_StdoutPathSymlinkedParentNotFollowed(t *testing.T) {
	cfg := newTestConfig(t)
	outsideDir := t.TempDir()

	res := captureRun(t, cfg, "out/answer.txt", "/bin/ln", "-s", outsideDir, "out")

	if _, err := os.Lstat(filepath.Join(outsideDir, "answer.txt")); err == nil {
		t.Fatalf("the agent wrote the capture through the symlinked directory %s", outsideDir)
	}
	if !strings.Contains(res.Error, "stdout_path") {
		t.Errorf("Error = %q, want the refused stdout_path copy reported", res.Error)
	}
}

// TestCopyCapture_DestinationRefusalsAreCommandCaused asserts that every
// destination the command can shape into an unusable one is refused with
// errCaptureRefused, which marks the failure as caused by the command
// rather than by the agent. A FIFO is refused because a copy into a FIFO
// with no reader blocks once the pipe buffer fills, which would hold the
// activity until core times it out.
func TestCopyCapture_DestinationRefusalsAreCommandCaused(t *testing.T) {
	cases := map[string]struct {
		rel     string
		prepare func(t *testing.T, root string)
	}{
		"symlink": {"answer.txt", func(t *testing.T, root string) {
			mustSymlink(t, filepath.Join(t.TempDir(), "x"), filepath.Join(root, "answer.txt"))
		}},
		"dangling symlink in workspace": {"answer.txt", func(t *testing.T, root string) {
			mustSymlink(t, "missing.txt", filepath.Join(root, "answer.txt"))
		}},
		"symlinked parent": {"out/answer.txt", func(t *testing.T, root string) {
			mustSymlink(t, t.TempDir(), filepath.Join(root, "out"))
		}},
		"directory": {"answer.txt", func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "answer.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		"file as parent": {"out/answer.txt", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "out"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		"fifo": {"answer.txt", func(t *testing.T, root string) {
			if err := syscall.Mkfifo(filepath.Join(root, "answer.txt"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			capture := filepath.Join(t.TempDir(), "stdout")
			if err := os.WriteFile(capture, []byte("student bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			tc.prepare(t, root)

			err := copyCaptureWithin(t, capture, root, root, tc.rel)
			if !errors.Is(err, errCaptureRefused) {
				t.Fatalf("copyCapture = %v, want errCaptureRefused", err)
			}
		})
	}
}

// TestCopyCapture_CaptureReplacedBySymlinkRefused asserts that the capture
// file is read without following a symlink. The staging directory sits
// where a same-user command can reach it, so a command could otherwise swap
// its own capture for a link to a file only the agent can read.
func TestCopyCapture_CaptureReplacedBySymlinkRefused(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("agent only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "stdout")
	mustSymlink(t, secret, capture)

	err := copyCaptureWithin(t, capture, root, root, "answer.txt")
	if !errors.Is(err, errCaptureRefused) {
		t.Fatalf("copyCapture = %v, want errCaptureRefused", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "answer.txt")); err == nil {
		t.Error("the symlinked capture was copied into the workspace")
	}
}

// TestCopyCapture_ReplacesRegularFileAndCreatesParents asserts the normal
// contract: missing parent directories are created, and a regular file
// already at the destination is truncated and rewritten.
func TestCopyCapture_ReplacesRegularFileAndCreatesParents(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "stage")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "stdout")
	if err := os.WriteFile(capture, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCaptureWithin(t, capture, root, workdir, "a/b/out.txt"); err != nil {
		t.Fatalf("copyCapture into fresh parents: %v", err)
	}
	dest := filepath.Join(workdir, "a/b/out.txt")
	if data, _ := os.ReadFile(dest); string(data) != "new\n" {
		t.Errorf("destination = %q, want %q", data, "new\n")
	}

	if err := os.WriteFile(dest, []byte("a much longer previous content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyCaptureWithin(t, capture, root, workdir, "a/b/out.txt"); err != nil {
		t.Fatalf("copyCapture over a regular file: %v", err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "new\n" {
		t.Errorf("destination = %q, want %q with the previous content truncated", data, "new\n")
	}
}

// copyCaptureWithin calls copyCapture and fails the test if it does not
// return within a few seconds, so a destination that blocks the open is
// reported as a failure rather than hanging the test binary.
func copyCaptureWithin(t *testing.T, capture, root, workdir, rel string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- copyCapture(capture, root, workdir, rel) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("copyCapture blocked on %s", rel)
		return nil
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
