// Package cpuquota pins GOMAXPROCS to the container's CPU quota so ghost's
// own runtime threads track the CPUs it may use rather than the host's core
// count.
package cpuquota

import (
	"math"

	"go.uber.org/automaxprocs/maxprocs"
)

// PinGOMAXPROCS sets GOMAXPROCS to min(NumCPU, max(ceil(quota), 2)), the
// formula Go 1.25 applies natively, and returns an error when the quota
// cannot be read; GOMAXPROCS is then left at its default. A pre-set
// GOMAXPROCS environment variable takes precedence. With the default of one
// P per host core, a throttled container carries scheduler and timer work for
// cores it cannot use, which delays the agent's Temporal heartbeat, and the
// idle garbage-collection workers can occupy a thread per P, every one of
// them charged to RLIMIT_NPROC alongside the student command. The go
// directive is below 1.25 and an explicit pin disables the native tracking,
// so this call stays until the directive moves. logf, when not nil, receives
// one line describing the value chosen.
func PinGOMAXPROCS(logf func(format string, args ...any)) error {
	opts := []maxprocs.Option{
		maxprocs.RoundQuotaFunc(func(q float64) int { return int(math.Ceil(q)) }),
		maxprocs.Min(2),
	}
	if logf != nil {
		opts = append(opts, maxprocs.Logger(logf))
	}
	_, err := maxprocs.Set(opts...)
	return err
}
