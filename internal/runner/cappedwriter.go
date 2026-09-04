package runner

import (
	"io"
	"sync"
	"sync/atomic"
)

// outputBudget is a byte budget shared across the stdout and stderr capped
// writers of a single supervised execution. It mirrors the host watchdog's
// historical semantics, which capped the *total* /output footprint (sum of
// both files) against maxOutputDirBytes, not each file independently.
type outputBudget struct {
	mu        sync.Mutex
	remaining int64
	truncated atomic.Bool
}

// newOutputBudget creates a budget allowing up to maxBytes total across all
// writers sharing it.
func newOutputBudget(maxBytes int64) *outputBudget {
	return &outputBudget{remaining: maxBytes}
}

// Truncated reports whether any writer sharing this budget dropped bytes.
func (b *outputBudget) Truncated() bool { return b.truncated.Load() }

// cappedWriter writes through to an underlying file (e.g. /output/stdout) until
// the shared budget is exhausted, then silently drops further bytes while still
// reporting them as written. Adapted from zinc's LimitedBuffer, but passthrough
// (writes to the file) instead of buffering in memory.
type cappedWriter struct {
	w      io.Writer     // the /output/{stdout|stderr} file
	budget *outputBudget // shared total cap across stdout+stderr
}

// newCappedWriter wraps w with the shared budget.
func newCappedWriter(w io.Writer, budget *outputBudget) *cappedWriter {
	return &cappedWriter{w: w, budget: budget}
}

// Write enforces the cap as it writes. It always reports len(p) so the
// child's write never short-writes, blocks, or errors once the cap is hit.
// Excess bytes are dropped and the shared truncated flag is set, the same
// discard-but-report-success behavior as zinc's LimitedBuffer.
func (c *cappedWriter) Write(p []byte) (int, error) {
	c.budget.mu.Lock()
	if c.budget.remaining <= 0 {
		c.budget.mu.Unlock()
		c.budget.truncated.Store(true)
		return len(p), nil
	}
	if int64(len(p)) > c.budget.remaining {
		allowed := c.budget.remaining
		c.budget.remaining = 0
		c.budget.mu.Unlock()
		n, err := c.w.Write(p[:allowed])
		c.budget.truncated.Store(true)
		if err != nil {
			return n, err
		}
		// Report the full length so the child never sees a short write.
		return len(p), nil
	}
	c.budget.remaining -= int64(len(p))
	c.budget.mu.Unlock()
	return c.w.Write(p)
}
