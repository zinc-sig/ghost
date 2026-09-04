//go:build linux

package sandbox

import (
	"os"
	"strconv"
	"strings"
)

// cgroup v2 files, read from the cgroupns root. Inside a private cgroup
// namespace the executor's own subtree appears at /sys/fs/cgroup, scoped to
// exactly this container. No /proc/<pid>/cgroup path resolution is needed.
const (
	memoryCurrentPath = "/sys/fs/cgroup/memory.current"
	memoryPeakPath    = "/sys/fs/cgroup/memory.peak"
	memoryEventsPath  = "/sys/fs/cgroup/memory.events"
)

// CgroupV2Available reports whether cgroup v2 memory accounting is readable at
// the cgroupns root. It is the gate for in-container peak/OOM sampling: when
// false (cgroup v1, hybrid, or an unexpected layout) the supervisor emits a
// zero peak rather than failing. RFD 0016 scopes cluster backends to cgroup-v2
// nodes, so a false here is a dev-host degradation only.
func CgroupV2Available() bool {
	if _, ok := readInt64File(memoryCurrentPath); ok {
		return true
	}
	if _, ok := readInt64File(memoryPeakPath); ok {
		return true
	}
	return false
}

// ReadMemoryCurrent reads /sys/fs/cgroup/memory.current. The bool is false on
// any read/parse error so callers can distinguish a true zero from a failure.
func ReadMemoryCurrent() (int64, bool) {
	return readInt64File(memoryCurrentPath)
}

// ReadMemoryPeak reads the kernel memory watermark /sys/fs/cgroup/memory.peak.
// Inside the container this file is read-only (RFD 0016 Decision 4): ghost can
// read it but never reset it, hence the baseline-delta logic in the supervisor.
func ReadMemoryPeak() (int64, bool) {
	return readInt64File(memoryPeakPath)
}

// ReadOOMKillCount parses the oom_kill counter from /sys/fs/cgroup/memory.events
// (cgroup v2). It returns 0 on a missing file or parse failure, which makes a
// before/after comparison report "no OOM", the safe default.
func ReadOOMKillCount() int64 {
	data, err := os.ReadFile(memoryEventsPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == "oom_kill" {
			count, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0
			}
			return count
		}
	}
	return 0
}

// readInt64File reads a single-integer cgroup file (TrimSpace + base-10 parse),
// mirroring the core monitor's parse logic.
func readInt64File(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
