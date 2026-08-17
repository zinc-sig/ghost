package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// slowUploadStore wraps fakeStore with a per-upload delay, modeling a slow
// object-storage backend during the post-exec upload phase.
type slowUploadStore struct {
	*fakeStore
	delay time.Duration
}

func (s *slowUploadStore) UploadFile(ctx context.Context, bucket, key, path string) error {
	time.Sleep(s.delay)
	return s.fakeStore.UploadFile(ctx, bucket, key, path)
}

// TestRunExec_HeartbeatsThroughUploadPhase pins the backend/12 upload-window
// fix: heartbeats must keep flowing WHILE the post-exec capture uploads run,
// not stop when the child exits. Before the fix the heartbeat goroutine was
// closed before the uploads, so a slow storage backend left the activity
// heartbeat-dark exactly when the exec had already consumed its window —
// presenting as a spurious server-side heartbeat timeout on a live container.
//
// Mechanism: a near-instant exec (so the exec phase contributes ~no ticks)
// followed by two slow uploads (stdout + stderr, 400ms each) with a 50ms
// tick. Any substantial number of observed heartbeats can therefore only
// have been recorded during the upload phase.
func TestRunExec_HeartbeatsThroughUploadPhase(t *testing.T) {
	origInterval := heartbeatInterval
	heartbeatInterval = 150 * time.Millisecond
	defer func() { heartbeatInterval = origInterval }()

	cfg := newTestConfig(t)
	store := &slowUploadStore{fakeStore: newFakeStore(), delay: 500 * time.Millisecond}
	env := newActivityEnv(t, cfg, store)

	var beats atomic.Int64
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, _ converter.EncodedValues) {
		beats.Add(1)
	})

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Stage:           "test",
		ScenarioCode:    "q1",
		Spec: contract.ExecSpec{
			Command: "/bin/echo",
			Args:    []string{"hb"},
			Workdir: ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q1"},
	}
	res := execRun(t, env, input)
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}

	// WHY ">= 1", not a tick count: the SDK BATCHES RecordHeartbeat calls
	// (the exact mechanism backend/12 turned on) — the test env's invoker
	// uses the default 30s throttle regardless of worker options, so the
	// listener observes WIRE SENDS, not ticks: the first tick sends
	// immediately, every later tick coalesces into a window that outlives
	// the test. The timing makes one send fully discriminating: the echo
	// exec finishes in a few ms, far inside the 150ms first tick — so
	// WITHOUT the defer fix the ticker goroutine is closed before it ever
	// fires and the listener sees exactly 0 sends; WITH the fix the first
	// tick lands ~150ms into the ~1s upload phase and sends. Any recorded
	// heartbeat therefore proves ticking continued through the uploads.
	if got := beats.Load(); got < 1 {
		t.Fatalf("recorded %d heartbeats — heartbeats did not continue through the upload phase", got)
	}
}
