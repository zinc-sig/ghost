//go:build linux

package reaper

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// reaper reaps zombie children as PID 1 (container init) AND delivers each
// reaped child's wait status to whoever is waiting on that PID. The agent
// runs one os/exec child per scenario and calls cmd.Wait() on it; without
// status delivery the reaper's Wait4(-1) races cmd.Wait() and steals the
// child, so cmd.Wait() fails with ECHILD ("no child processes") and the exec
// is wrongly reported as "could not spawn". With delivery, the exec recovers
// the real status via WaitChild on ECHILD.
type reaper struct {
	mu      sync.Mutex
	pending map[int]syscall.WaitStatus      // reaped before anyone waited
	waiters map[int]chan syscall.WaitStatus // registered waiters by pid
}

var (
	once   sync.Once
	global *reaper
)

// Start begins reaping in a background goroutine, but only when this process
// is PID 1. Safe to call multiple times. When not PID 1 the reaper is inert
// (os/exec.Wait reaps direct children itself, with no competitor).
func Start() {
	if os.Getpid() != 1 {
		return
	}
	once.Do(func() {
		global = &reaper{
			pending: make(map[int]syscall.WaitStatus),
			waiters: make(map[int]chan syscall.WaitStatus),
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGCHLD)
		go func() {
			for range ch {
				for {
					var ws syscall.WaitStatus
					wpid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
					if wpid <= 0 || err != nil {
						break
					}
					global.deliver(wpid, ws)
				}
			}
		}()
	})
}

func (r *reaper) deliver(pid int, ws syscall.WaitStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.waiters[pid]; ok {
		ch <- ws
		delete(r.waiters, pid)
		return
	}
	// No waiter yet (reaped before WaitChild registered, or a true orphan).
	// Buffer it; WaitChild collects it. Orphans with no waiter linger here
	// until the (short-lived) container is torn down, which bounds the leak.
	r.pending[pid] = ws
}

// WaitChild blocks until pid has been reaped and returns its wait status.
// ok is false when the reaper is inert (not PID 1). The caller should use
// os/exec.Wait, which reaps the child itself in that case.
func WaitChild(pid int) (ws syscall.WaitStatus, ok bool) {
	if global == nil {
		return 0, false
	}
	global.mu.Lock()
	if buffered, found := global.pending[pid]; found {
		delete(global.pending, pid)
		global.mu.Unlock()
		return buffered, true
	}
	ch := make(chan syscall.WaitStatus, 1)
	global.waiters[pid] = ch
	global.mu.Unlock()
	return <-ch, true
}
