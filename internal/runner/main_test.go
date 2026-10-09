package runner

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// execChildEnv names the environment variable that turns a re-executed test
// binary into an ExecuteExec child. Its value is the path of a JSON-encoded
// Config; the child writes the error ExecuteExec returns to that path plus
// ".err".
const execChildEnv = "GHOST_TEST_EXEC_CHILD_CONFIG"

func TestMain(m *testing.M) {
	if path := os.Getenv(execChildEnv); path != "" {
		os.Exit(runExecChild(path))
	}
	os.Exit(m.Run())
}

// runExecChild calls ExecuteExec in the re-executed test binary. ExecuteExec
// points the process's own stdin, stdout and stderr at the capture files
// before it can fail, so its error goes to a file rather than to stderr.
func runExecChild(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 2
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return 2
	}
	if err := ExecuteExec(&config); err != nil {
		if os.WriteFile(path+".err", []byte(err.Error()), 0o600) != nil {
			return 2
		}
	}
	return 0
}

// executeExecInChild runs ExecuteExec in a copy of the test binary and returns
// the error it reported, or nil if it exec'd the command. Calling ExecuteExec
// in the test process would leave the test binary's stdout and stderr dup3'd
// onto the capture files, so every later test's output, including its
// failures and the package's final result, would land in a temp file.
func executeExecInChild(t *testing.T, config *Config) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "exec-config.json")
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), execChildEnv+"="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("exec child: %v\n%s", err, out)
	}
	msg, err := os.ReadFile(path + ".err")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read child error: %v", err)
	}
	return errors.New(string(msg))
}
