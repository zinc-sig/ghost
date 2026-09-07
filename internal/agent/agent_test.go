package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// ghostBin is the real ghost binary built once by TestMain. RunExec
// spawns it as the sandboxed child (`ghost exec ...`), so these
// tests exercise the actual cmd/exec.go flag surface, not a stub.
var ghostBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ghost-agent-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	ghostBin = filepath.Join(tmp, "ghost")

	// Orphans of test children reparent to this process, as they do to
	// the agent as pid 1, so the orphan sweep tests see them.
	if err := enableChildSubreaper(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to enable child subreaper: %v\n", err)
		os.Exit(1)
	}

	// GHOST_TEST_BIN points at a prebuilt ghost binary so the suite can run
	// where no Go toolchain is present, such as inside a memory-capped
	// container that exercises the kernel out-of-memory path.
	if prebuilt := os.Getenv("GHOST_TEST_BIN"); prebuilt != "" {
		ghostBin = prebuilt
	} else {
		build := exec.Command("go", "build", "-o", ghostBin, ".")
		build.Dir = "../.." // module root
		if out, err := build.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build ghost binary: %v\n%s", err, out)
			_ = os.RemoveAll(tmp)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// fakeStore is an in-memory ObjectStore. Downloads serve from objects;
// uploads are recorded in uploads keyed "bucket/key". It reuses
// materializeObject so the traversal defence is identical to the real
// minio-backed store.
type fakeStore struct {
	mu        sync.Mutex
	objects   map[string]map[string][]byte // bucket -> key -> data
	uploads   map[string][]byte            // "bucket/key" -> data
	uploadErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		objects: make(map[string]map[string][]byte),
		uploads: make(map[string][]byte),
	}
}

func (f *fakeStore) UploadFile(_ context.Context, bucket, key, path string) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[bucket+"/"+key] = data
	return nil
}

func (f *fakeStore) UploadBytes(_ context.Context, bucket, key string, data []byte) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[bucket+"/"+key] = append([]byte(nil), data...)
	return nil
}

func (f *fakeStore) DownloadPrefix(_ context.Context, bucket, prefix, targetDir string) (int, int64, error) {
	f.mu.Lock()
	keys := make([]string, 0)
	for key := range f.objects[bucket] {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	f.mu.Unlock()
	sort.Strings(keys)

	files := 0
	var total int64
	for _, key := range keys {
		n, err := materializeObject(targetDir, prefix, key, bytes.NewReader(f.objects[bucket][key]))
		if err != nil {
			return files, total, err
		}
		files++
		total += n
	}
	return files, total, nil
}

func (f *fakeStore) upload(bucket, key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.uploads[bucket+"/"+key]
	return data, ok
}

// newTestConfig returns an agent config suitable for tests: sandbox off
// (Landlock/netns are unavailable in most test environments), no pid
// limit (RLIMIT_NPROC counts every process of the test user).
func newTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Workdir:        t.TempDir(),
		StagingDir:     t.TempDir(),
		DefaultTimeout: 30 * time.Second,
		// The production default cap, so every test exec exercises the
		// --max-file-bytes plumbing the way real runs do.
		DefaultOutputLimit: 64 << 20,
		MaxPids:            0,
		Sandbox:            false,
		GhostPath:          ghostBin,
		AgentVersion:       "test",
	}
}

// newActivityEnv registers both activities by their contract names on a
// Temporal test activity environment.
func newActivityEnv(t *testing.T, cfg *Config, store ObjectStore) *testsuite.TestActivityEnvironment {
	t.Helper()
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestActivityEnvironment()
	env.SetTestTimeout(60 * time.Second)
	acts := NewActivities(cfg, store)
	env.RegisterActivityWithOptions(acts.FetchSubmission, activity.RegisterOptions{Name: contract.FetchSubmissionActivity})
	env.RegisterActivityWithOptions(acts.RunExec, activity.RegisterOptions{Name: contract.RunExecActivity})
	return env
}

func strPtr(s string) *string { return &s }
