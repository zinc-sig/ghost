package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zinc-sig/ghost/internal/agent/contract"
	"github.com/zinc-sig/ghost/internal/sandbox"
)

// agentOnlyDir returns a directory outside every path the Landlock ruleset
// lets a command read or write, so a file in it is readable by the agent
// and not by a sandboxed command. The test's working directory is the
// package directory in the source tree, which is outside those paths.
func agentOnlyDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".agent-only-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestRunExec_SandboxedCommandCannotSwapItsCapture asserts that a
// Landlocked command cannot replace its stdout capture with a link to a
// file only the agent can read: with the staging area outside every path
// the command may write, the swap fails and the upload carries the
// command's own output. With the staging area under /tmp, which Landlock
// lets the command write, the agent uploads the linked file as stdout.
func TestRunExec_SandboxedCommandCannotSwapItsCapture(t *testing.T) {
	if !sandbox.LandlockAvailable() {
		t.Skip("Landlock is not available on this kernel")
	}
	secret := filepath.Join(agentOnlyDir(t), "secret")
	if err := os.WriteFile(secret, []byte("agent-only-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(t)
	cfg.Sandbox = true
	cfg.StagingDir = agentOnlyDir(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	// The command finds its own capture by the inode its stdout points at
	// and replaces it with a link to the secret.
	script := `for f in "$STAGING"/exec-*/stdout; do rm -f "$f" && ln -s "$SECRET" "$f"; done; echo own-output`
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command: "/bin/sh",
			Args:    []string{"-c", script},
			Workdir: ".",
			Env:     map[string]string{"STAGING": cfg.StagingDir, "SECRET": secret},
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/swap"},
	}
	res := execRun(t, env, input)

	data, ok := store.upload("runs", "55/test/swap/stdout")
	if strings.Contains(string(data), "agent-only-secret") {
		t.Fatalf("the agent uploaded the linked agent-only file as stdout: %q", data)
	}
	if !ok || string(data) != "own-output\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want the command's own output (error: %q)", data, ok, res.Error)
	}
}

// TestRunExec_StdinPathSymlinkNotFollowed asserts that a workspace stdin
// path is opened without following a symbolic link an earlier command
// left there. Following it would feed the command, and upload as the stdin
// artifact, a file only the agent can read.
func TestRunExec_StdinPathSymlinkNotFollowed(t *testing.T) {
	secret := filepath.Join(agentOnlyDir(t), "secret")
	if err := os.WriteFile(secret, []byte("agent-only-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(t)
	mustSymlink(t, secret, filepath.Join(cfg.Workdir, "input.txt"))
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	stdin := "input.txt"
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/cat", Workdir: ".", StdinPath: &stdin},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/stdin"},
	}
	res := execRun(t, env, input)

	for _, key := range []string{"stdout", "stdin"} {
		if data, _ := store.upload("runs", "55/test/stdin/"+key); strings.Contains(string(data), "agent-only-secret") {
			t.Errorf("the linked agent-only file reached the %s artifact: %q", key, data)
		}
	}
	if res.ExitCode != nil {
		t.Errorf("ExitCode = %s, want nil: the command must not run on a refused stdin", fmtExitCode(res.ExitCode))
	}
	if !strings.Contains(res.Error, "stdin path") {
		t.Errorf("Error = %q, want the refused stdin path reported", res.Error)
	}
}

// TestRunExec_StdinPathCopiedFromWorkspace asserts the normal stdin path
// contract still holds: a regular workspace file, also through a
// subdirectory, feeds the command and is uploaded as the stdin artifact.
func TestRunExec_StdinPathCopiedFromWorkspace(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(filepath.Join(cfg.Workdir, "in"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Workdir, "in/case1.txt"), []byte("3 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	stdin := "in/case1.txt"
	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/cat", Workdir: ".", StdinPath: &stdin},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/stdin-ok"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 || res.Error != "" {
		t.Fatalf("ExitCode = %s, Error = %q, want 0 and none", fmtExitCode(res.ExitCode), res.Error)
	}
	for _, key := range []string{"stdout", "stdin"} {
		if data, _ := store.upload("runs", "55/test/stdin-ok/"+key); string(data) != "3 4\n" {
			t.Errorf("%s artifact = %q, want %q", key, data, "3 4\n")
		}
	}
}

// TestRunExec_WorkdirSymlinkNotFollowed asserts that creating the
// effective workdir does not follow a symbolic link an earlier command left
// in the workspace, which would let it make the agent create directories
// outside the workspace.
func TestRunExec_WorkdirSymlinkNotFollowed(t *testing.T) {
	cfg := newTestConfig(t)
	outside := t.TempDir()
	mustSymlink(t, outside, filepath.Join(cfg.Workdir, "build"))
	env := newActivityEnv(t, cfg, newFakeStore())

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/true", Workdir: "build/stage2"},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/workdir"},
	}
	res := execRun(t, env, input)

	if _, err := os.Lstat(filepath.Join(outside, "stage2")); err == nil {
		t.Fatal("the agent created the workdir through the symlink, outside the workspace")
	}
	if res.ExitCode != nil || !strings.Contains(res.Error, "workdir") {
		t.Errorf("ExitCode = %s, Error = %q, want no run and the refused workdir reported", fmtExitCode(res.ExitCode), res.Error)
	}
}

// TestCheckStagingUnreachable asserts that, with the sandbox on, the agent
// refuses a staging directory a command may write, which is any path
// under the Landlock read-write set, and accepts one outside it. A staging
// directory a command can write lets it plant or swap the files the agent
// and the pre-Landlock child open there.
func TestCheckStagingUnreachable(t *testing.T) {
	workdir := t.TempDir()
	outside := agentOnlyDir(t)
	tests := []struct {
		name    string
		staging string
		sandbox bool
		wantErr bool
	}{
		{name: "outside the read-write set", staging: outside, sandbox: true},
		{name: "under /tmp", staging: filepath.Join(os.TempDir(), "ghost-agent-staging-x"), sandbox: true, wantErr: true},
		{name: "inside the workspace", staging: filepath.Join(workdir, ".staging"), sandbox: true, wantErr: true},
		{name: "under /dev", staging: "/dev/shm/ghost", sandbox: true, wantErr: true},
		{name: "under /tmp with the sandbox off", staging: filepath.Join(os.TempDir(), "x"), sandbox: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Workdir: workdir, StagingDir: tt.staging, Sandbox: tt.sandbox}
			err := checkStagingUnreachable(cfg)
			if tt.wantErr != (err != nil) {
				t.Fatalf("checkStagingUnreachable() = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), EnvStagingDir) {
				t.Errorf("error %q should name %s", err, EnvStagingDir)
			}
		})
	}
}

// TestDefaultStagingDir asserts that the default staging directory is
// created 0700 under the first usable candidate root, skipping a root it
// cannot create, and that no candidate leaves an error behind.
func TestDefaultStagingDir(t *testing.T) {
	usable := agentOnlyDir(t)
	saved := stagingRoots
	stagingRoots = func() []string { return []string{"/proc/ghost-agent-not-creatable", usable} }
	t.Cleanup(func() { stagingRoots = saved })

	dir, err := defaultStagingDir()
	if err != nil {
		t.Fatalf("defaultStagingDir() = %v", err)
	}
	if filepath.Dir(dir) != usable {
		t.Errorf("staging dir %s, want one under %s", dir, usable)
	}
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("staging dir mode = %v (err %v), want 0700", st.Mode().Perm(), err)
	}

	stagingRoots = func() []string { return []string{"/proc/ghost-agent-not-creatable"} }
	if _, err := defaultStagingDir(); err == nil || !strings.Contains(err.Error(), EnvStagingDir) {
		t.Errorf("defaultStagingDir() with no usable root = %v, want an error naming %s", err, EnvStagingDir)
	}
}

// TestStagingRefusalErrorsAreCommandCaused asserts that a refused stdin
// path or workdir wraps errWorkspacePathRefused, the sentinel that marks
// the failure as caused by the command.
func TestStagingRefusalErrorsAreCommandCaused(t *testing.T) {
	root := t.TempDir()
	mustSymlink(t, t.TempDir(), filepath.Join(root, "link"))
	if _, err := openBeneath(root, "link/input.txt"); !errors.Is(err, errWorkspacePathRefused) {
		t.Errorf("openBeneath through a symlinked directory = %v, want errWorkspacePathRefused", err)
	}
	if err := mkdirBeneath(root, "link/stage", 0o755); !errors.Is(err, errWorkspacePathRefused) {
		t.Errorf("mkdirBeneath through a symlinked directory = %v, want errWorkspacePathRefused", err)
	}
}
