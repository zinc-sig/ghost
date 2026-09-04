//go:build linux

package sandbox

import "testing"

// requireCgroupV2 skips the test unless cgroup v2 memory accounting is readable
// at the cgroupns root. On hosts/CI without it (no private cgroupns, cgroup v1,
// hybrid) the test skips rather than fails, mirroring ghost's Docker-unavailable
// skip convention.
func requireCgroupV2(t *testing.T) {
	t.Helper()
	if !CgroupV2Available() {
		t.Skip("cgroup v2 memory accounting not available at cgroupns root")
	}
}

func TestReadMemoryCurrent(t *testing.T) {
	requireCgroupV2(t)
	v, ok := ReadMemoryCurrent()
	if !ok {
		t.Fatal("ReadMemoryCurrent returned ok=false on a cgroup-v2 host")
	}
	if v < 0 {
		t.Errorf("memory.current must be non-negative, got %d", v)
	}
}

func TestReadMemoryPeak(t *testing.T) {
	requireCgroupV2(t)
	v, ok := ReadMemoryPeak()
	if !ok {
		t.Fatal("ReadMemoryPeak returned ok=false on a cgroup-v2 host")
	}
	if v < 0 {
		t.Errorf("memory.peak must be non-negative, got %d", v)
	}
}

func TestReadOOMKillCount(t *testing.T) {
	requireCgroupV2(t)
	// On a healthy host the live cgroup should not have been OOM-killed; the
	// counter is non-negative regardless.
	if c := ReadOOMKillCount(); c < 0 {
		t.Errorf("oom_kill count must be non-negative, got %d", c)
	}
}
