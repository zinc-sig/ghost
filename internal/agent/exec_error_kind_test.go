package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// TestRunExec_ErrorKind pins how RunExec classifies the failures it reports
// on ExecResult.Error. Core retries a run whose exec reports an infra error
// and scores a command error as the formula says, so a failure the command
// can cause on purpose must never be infra, and a failure of the agent or
// the object store must never be command.
func TestRunExec_ErrorKind(t *testing.T) {
	answer := "answer.txt"
	nested := "out/answer.txt"
	escape := "../answer.txt"
	workdirItself := "."
	stdinFile := "input.txt"

	cases := []struct {
		name string
		// setup prepares the config, the store, and the workspace before the
		// exec runs.
		setup    func(t *testing.T, cfg *Config, store *fakeStore)
		spec     contract.ExecSpec
		wantKind contract.ErrorKind
	}{
		{
			name:     "a clean exec reports no error",
			spec:     contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: ".", StdoutPath: &answer},
			wantKind: "",
		},
		{
			name: "an object store upload failure is infra",
			setup: func(_ *testing.T, _ *Config, store *fakeStore) {
				store.uploadErr = errors.New("connection reset by peer")
			},
			spec:     contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: "."},
			wantKind: contract.ErrorKindInfra,
		},
		{
			name: "a failure to spawn the sandboxed child is infra",
			setup: func(_ *testing.T, cfg *Config, _ *fakeStore) {
				cfg.GhostPath = filepath.Join(cfg.StagingDir, "missing-ghost")
			},
			spec:     contract.ExecSpec{Command: "/bin/true", Workdir: "."},
			wantKind: contract.ErrorKindInfra,
		},
		{
			name: "a directory the command made at the capture path is command",
			// Stand-in for a submitted program that exits 0 after creating a
			// directory where the pipeline copies its stdout.
			spec:     contract.ExecSpec{Command: "/bin/mkdir", Args: []string{"-p", answer}, Workdir: ".", StdoutPath: &answer},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name: "a file the command made where the capture's parent directory goes is command",
			spec: contract.ExecSpec{Command: "/bin/touch", Args: []string{"out"}, Workdir: ".", StdoutPath: &nested},
			// The copy needs out/ as a directory.
			wantKind: contract.ErrorKindCommand,
		},
		{
			name: "a file left where the workdir goes is command",
			setup: func(t *testing.T, cfg *Config, _ *fakeStore) {
				if err := os.WriteFile(filepath.Join(cfg.Workdir, "sub"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			spec:     contract.ExecSpec{Command: "/bin/true", Workdir: "sub/inner"},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name:     "a capture path outside the workspace is command",
			spec:     contract.ExecSpec{Command: "/bin/true", Workdir: ".", StdoutPath: &escape},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name:     "a capture path naming the workdir itself is command",
			spec:     contract.ExecSpec{Command: "/bin/true", Workdir: ".", StdoutPath: &workdirItself},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name: "a capture the command swapped for a symlink is command",
			// With the sandbox off the command can reach its staging
			// session, and replaces its own stdout capture with a link.
			setup: func(t *testing.T, cfg *Config, _ *fakeStore) {
				secret := filepath.Join(t.TempDir(), "secret")
				if err := os.WriteFile(secret, []byte("agent only\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("ZZ_STAGING", cfg.StagingDir)
				t.Setenv("ZZ_SECRET", secret)
			},
			spec: contract.ExecSpec{
				Command: "/bin/sh",
				Args:    []string{"-c", `for f in "$ZZ_STAGING"/exec-*/stdout; do rm -f "$f" && ln -s "$ZZ_SECRET" "$f"; done`},
				Workdir: ".", StdoutPath: &answer,
			},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name: "a stdin path an earlier command left as a symlink is command",
			setup: func(t *testing.T, cfg *Config, _ *fakeStore) {
				mustSymlink(t, filepath.Join(t.TempDir(), "secret"), filepath.Join(cfg.Workdir, stdinFile))
			},
			spec:     contract.ExecSpec{Command: "/bin/cat", Workdir: ".", StdinPath: &stdinFile},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name:     "a stdin path naming a missing workspace file is command",
			spec:     contract.ExecSpec{Command: "/bin/cat", Workdir: ".", StdinPath: &stdinFile},
			wantKind: contract.ErrorKindCommand,
		},
		{
			name: "an upload failure outranks a command error in the same exec",
			setup: func(_ *testing.T, _ *Config, store *fakeStore) {
				store.uploadErr = errors.New("connection reset by peer")
			},
			spec:     contract.ExecSpec{Command: "/bin/mkdir", Args: []string{"-p", answer}, Workdir: ".", StdoutPath: &answer},
			wantKind: contract.ErrorKindInfra,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			store := newFakeStore()
			if tc.setup != nil {
				tc.setup(t, cfg, store)
			}
			env := newActivityEnv(t, cfg, store)

			res := execRun(t, env, contract.RunExecInput{
				ProtocolVersion: contract.ProtocolVersion,
				Spec:            tc.spec,
				StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q1"},
			})

			if res.ErrorKind != tc.wantKind {
				t.Errorf("ErrorKind = %q, want %q (Error = %q)", res.ErrorKind, tc.wantKind, res.Error)
			}
			if (res.Error == "") != (tc.wantKind == "") {
				t.Errorf("Error = %q with ErrorKind %q: an error and its kind are set together", res.Error, res.ErrorKind)
			}
		})
	}
}

// TestStdinErrorKind pins the stdin classification that the end-to-end
// cases cannot reach: a failure writing the staged copy is on the agent's
// staging path, so it is infra even when its errno is one a command could
// cause elsewhere.
func TestStdinErrorKind(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(t.TempDir(), "exec-1", "stdin")
	cases := []struct {
		name string
		p    string
		err  error
		want contract.ErrorKind
	}{
		{"a refusal in the workspace", "input.txt",
			fmt.Errorf("%w: input.txt is a symbolic link", errWorkspacePathRefused), contract.ErrorKindCommand},
		{"a path escaping the workspace", "../input.txt", errors.New("escapes the workspace"), contract.ErrorKindCommand},
		{"a missing staging session", "input.txt",
			fmt.Errorf("failed to stage stdin: %w", &fs.PathError{Op: "open", Path: staged, Err: syscall.ENOENT}), contract.ErrorKindInfra},
		{"a full disk under the staging session", "input.txt",
			fmt.Errorf("failed to stage stdin: %w", &fs.PathError{Op: "write", Path: staged, Err: syscall.ENOSPC}), contract.ErrorKindInfra},
		{"a refused staging write", "input.txt",
			fmt.Errorf("failed to stage stdin: %w", &fs.PathError{Op: "open", Path: staged, Err: syscall.EACCES}), contract.ErrorKindInfra},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stdinErrorKind(root, tc.p, tc.err); got != tc.want {
				t.Errorf("stdinErrorKind(%q, %v) = %q, want %q", tc.p, tc.err, got, tc.want)
			}
		})
	}
}
