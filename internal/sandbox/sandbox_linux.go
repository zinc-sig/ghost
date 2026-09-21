//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"golang.org/x/sys/unix"
)

// ApplySandbox applies the Landlock filesystem restrictions shared by exec
// and supervise. Read-only: /usr, /bin, /lib, /lib64, /etc, /proc, and
// /sys/fs/cgroup, each ignored if missing. Read-write: /output, /tmp, /dev,
// and the given work directory.
func ApplySandbox(workDir string) error {
	if workDir == "" {
		return fmt.Errorf("sandbox: workDir must not be empty")
	}

	rules := []landlock.Rule{
		landlock.RODirs("/usr", "/bin", "/lib", "/lib64", "/etc").IgnoreIfMissing(),
		// /proc and /sys/fs/cgroup are readable so a container-aware runtime
		// such as the JVM finds its cgroup (through /proc/self/cgroup and
		// /proc/self/mountinfo) and sizes its heap and threads to the
		// container rather than the host. The grant covers all of /proc
		// because /proc/self is a different path in every process of the
		// command tree. Writes into /proc stay refused, so the oom_score_adj
		// set by exec holds, and the agent runs non-dumpable so its own
		// /proc entry stays unreadable. supervise's sampler reads the cgroup
		// files after this is applied.
		landlock.RODirs("/proc", "/sys/fs/cgroup").IgnoreIfMissing(),
		landlock.RWDirs("/output", "/tmp", "/dev", workDir),
	}

	if err := landlock.V5.BestEffort().RestrictPaths(rules...); err != nil {
		return fmt.Errorf("sandbox: landlock restrict paths: %w", err)
	}
	return nil
}

// LandlockAvailable reports whether the kernel supports Landlock (ABI >= 1).
// Used by tests to decide whether the sandbox actually enforces filesystem
// restrictions on this host; BestEffort no-ops when this is false.
func LandlockAvailable() bool {
	v, err := llsyscall.LandlockGetABIVersion()
	return err == nil && v >= 1
}

// EnforceMaxPids sets RLIMIT_NPROC for the current UID. The kernel counts
// every task of the UID against it, threads included, so ghost's own runtime
// threads take slots alongside the command's processes and threads. Core
// sizes the value with a base allowance that absorbs ghost's share.
func EnforceMaxPids(maxPids uint64) error {
	return unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{Cur: maxPids, Max: maxPids})
}

// EnforceMaxFileBytes sets RLIMIT_FSIZE so no single file written by the
// current process (or any descendant, since rlimits are inherited across fork
// and execve) can grow beyond maxBytes. A write that would extend a file past the
// limit delivers SIGXFSZ (default action: terminate); a process that ignores
// the signal gets EFBIG write errors instead, so the size bound holds either
// way. The limit applies per FILE, not to the process's total output.
//
// RLIMIT_CORE is zeroed first: SIGXFSZ's default action is terminate WITH a
// core dump, and the core file itself must not land in the workdir (it could
// be gigabytes, and later pipeline stages must never see it).
func EnforceMaxFileBytes(maxBytes uint64) error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return fmt.Errorf("sandbox: setrlimit RLIMIT_CORE: %w", err)
	}
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: maxBytes, Max: maxBytes}); err != nil {
		return fmt.Errorf("sandbox: setrlimit RLIMIT_FSIZE: %w", err)
	}
	return nil
}
