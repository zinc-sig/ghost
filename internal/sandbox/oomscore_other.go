//go:build !linux

package sandbox

import "fmt"

// MarkOOMVictim is not supported on non-Linux platforms.
func MarkOOMVictim() error {
	return fmt.Errorf("sandbox: oom_score_adj is only supported on Linux")
}
