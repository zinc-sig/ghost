package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// newTestContext returns a cancellable context for use in tests.
func newTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func TestHeartbeatWritesTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	hbFile := filepath.Join(tmpDir, ".heartbeat")

	// Override flags for test
	oldInterval := heartbeatInterval
	oldFile := heartbeatFile
	heartbeatInterval = 20 * time.Millisecond
	heartbeatFile = hbFile
	t.Cleanup(func() {
		heartbeatInterval = oldInterval
		heartbeatFile = oldFile
	})

	// Run heartbeat in a goroutine, cancel after we read a valid timestamp
	errCh := make(chan error, 1)
	cmd := heartbeatCmd
	ctx, cancel := newTestContext()
	defer cancel()
	cmd.SetContext(ctx)

	go func() {
		errCh <- heartbeatCommand(cmd, nil)
	}()

	// Poll for the heartbeat file
	var ts int64
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for heartbeat file")
		default:
		}

		data, err := os.ReadFile(hbFile)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		ts, err = strconv.ParseInt(string(data), 10, 64)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		break
	}

	// Cancel the heartbeat
	cancel()
	<-errCh

	// Verify the timestamp is recent (within 5 seconds)
	now := time.Now().Unix()
	if now-ts > 5 {
		t.Errorf("heartbeat timestamp too old: got %d, now %d (diff %ds)", ts, now, now-ts)
	}
	if ts > now {
		t.Errorf("heartbeat timestamp is in the future: got %d, now %d", ts, now)
	}
}

func TestHeartbeatGracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	hbFile := filepath.Join(tmpDir, ".heartbeat")

	oldInterval := heartbeatInterval
	oldFile := heartbeatFile
	heartbeatInterval = 20 * time.Millisecond
	heartbeatFile = hbFile
	t.Cleanup(func() {
		heartbeatInterval = oldInterval
		heartbeatFile = oldFile
	})

	cmd := heartbeatCmd
	ctx, cancel := newTestContext()
	cmd.SetContext(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- heartbeatCommand(cmd, nil)
	}()

	// Let it write at least one heartbeat
	time.Sleep(50 * time.Millisecond)

	// Signal shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("heartbeat returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat did not shut down within 2 seconds")
	}
}

// TestWriteHeartbeatCreatesParentDirs guards the fix for heartbeat failing on a
// nested --file path: writeHeartbeat must create missing parent directories
// (matching run/supervise's createFileWithDir), not error out on every write.
func TestWriteHeartbeatCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", ".heartbeat")
	if err := writeHeartbeat(path); err != nil {
		t.Fatalf("writeHeartbeat with nested path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("heartbeat file not created under nested dir: %v", err)
	}
	if _, err := strconv.ParseInt(string(data), 10, 64); err != nil {
		t.Errorf("heartbeat content %q is not a unix timestamp: %v", data, err)
	}
}

// writeHeartbeat must create the file owner-only (0600), not world-writable.
func TestWriteHeartbeatPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".heartbeat")
	if err := writeHeartbeat(path); err != nil {
		t.Fatalf("writeHeartbeat: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat heartbeat file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("heartbeat mode = %#o, want 0600", perm)
	}
}

// writeHeartbeat must also tighten a PRE-EXISTING broader-mode file it owns:
// O_CREAT's 0600 only applies on creation, so an existing 0666 .heartbeat is
// fchmod'd down to 0600 (the sandbox root-owned EPERM case is a tolerated no-op,
// not reachable in-process here since the test owns its temp file).
func TestWriteHeartbeatTightensPreexisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".heartbeat")
	if err := os.WriteFile(path, []byte("0"), 0o666); err != nil {
		t.Fatalf("pre-create: %v", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("pre-chmod: %v", err)
	}
	if err := writeHeartbeat(path); err != nil {
		t.Fatalf("writeHeartbeat: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat heartbeat file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("heartbeat mode = %#o after tightening a pre-existing 0666 file, want 0600", perm)
	}
}
