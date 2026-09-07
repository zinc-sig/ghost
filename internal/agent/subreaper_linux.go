//go:build linux

package agent

import "golang.org/x/sys/unix"

// enableChildSubreaper makes orphaned descendants of this process reparent
// to it instead of to init, so the orphan sweep after each exec sees a
// process that left its group with setsid. As pid 1 the agent is the
// reparent target anyway; the call makes an agent that is not pid 1 behave
// the same.
func enableChildSubreaper() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}
