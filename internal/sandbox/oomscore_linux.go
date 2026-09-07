//go:build linux

package sandbox

import (
	"fmt"
	"os"
)

// oomScoreAdjPath is the calling process's own out-of-memory score
// adjustment, inherited across fork and execve.
const oomScoreAdjPath = "/proc/self/oom_score_adj"

// MarkOOMVictim writes 1000 to the calling process's oom_score_adj so that,
// when the container's memory cap is hit, the kernel chooses a process in
// this process tree over the agent regardless of which process allocated.
// The value is inherited across execve and fork, so every descendant of the
// student command carries it. It must run before Landlock is applied,
// because Landlock refuses writes into /proc afterwards and that refusal is
// what stops the student process from lowering the value back. The caller
// treats a failure as a warning: an unwritable /proc leaves resident size
// ordering as the agent's only protection, which is a degraded posture and
// not a fault of the command. The full explanation is in the memory budgets
// section of internal/agent/README.md.
func MarkOOMVictim() error {
	if err := os.WriteFile(oomScoreAdjPath, []byte("1000"), 0); err != nil {
		return fmt.Errorf("sandbox: write %s: %w", oomScoreAdjPath, err)
	}
	return nil
}
