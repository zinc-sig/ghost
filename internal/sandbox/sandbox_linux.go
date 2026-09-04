//go:build linux

package sandbox

import (
	"fmt"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"golang.org/x/sys/unix"
)

// SandboxOpts enables opt-in Landlock grants that supervise needs beyond the
// base ruleset used by exec.
type SandboxOpts struct {
	// AllowCgroupRead grants RO /sys/fs/cgroup so supervise's sampler can read
	// memory.current/peak/events after Landlock is applied.
	AllowCgroupRead bool
}

// ApplySandbox applies the base Landlock filesystem restrictions.
// Read-only: /usr, /bin, /lib, /lib64, /etc (ignored if missing), /proc/self/fd.
// Read-write: /output, /tmp, /dev, and the given work directory.
func ApplySandbox(workDir string) error {
	return ApplySandboxWith(workDir, SandboxOpts{})
}

// ApplySandboxWith applies the base Landlock restrictions (see ApplySandbox)
// plus any grants enabled in opts.
func ApplySandboxWith(workDir string, opts SandboxOpts) error {
	if workDir == "" {
		return fmt.Errorf("sandbox: workDir must not be empty")
	}

	rules := []landlock.Rule{
		landlock.RODirs("/usr", "/bin", "/lib", "/lib64", "/etc").IgnoreIfMissing(),
		landlock.RODirs("/proc/self/fd"),
		landlock.RWDirs("/output", "/tmp", "/dev", workDir),
	}
	if opts.AllowCgroupRead {
		// RO + IgnoreIfMissing for non-cgroup-v2 hosts.
		rules = append(rules, landlock.RODirs("/sys/fs/cgroup").IgnoreIfMissing())
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

// EnforceMaxPids sets RLIMIT_NPROC to limit the total number of processes for
// the current user (UID). The limit counts ALL processes for the UID, including
// the ghost process itself. For example, with maxPids=33, ghost uses 1 slot and
// the student command can create up to 32 processes (including itself).
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
