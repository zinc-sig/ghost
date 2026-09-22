//go:build linux

package agent

import "golang.org/x/sys/unix"

// disableDumpable clears the process dumpable flag. The kernel then owns the
// agent's /proc/<pid> entry as root, refuses reads of its environ, maps, and
// mem to processes of the same UID, and writes no core dump. execve resets
// the flag, so children spawned by the agent are unaffected.
func disableDumpable() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
