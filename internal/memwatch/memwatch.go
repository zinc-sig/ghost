// Package memwatch enforces per-exec memory budgets: a sampler reads /proc,
// kills a watched exec past its budget, attributes kernel out-of-memory
// kills, and sweeps escapees. The agent watches every concurrent exec with
// it and supervise watches its one child, so a sandbox Run and a graded run
// apply the same budget rule to the same processes.
//
// An exec's members are its root process, every descendant of the root,
// and every process in the root's group, recomputed on every tick. A child
// that leaves the group with setsid or setpgid is still a descendant and
// stays budgeted while its parent chain to the root lives. An orphan in a
// group of its own (a descendant whose parent exited and that also left the
// exec's group, as a double fork into a new session makes) is charged only
// under supervise: supervise serves one exec and is a child subreaper (see
// EnableSubreaper), so the orphan is reparented to it and every descendant
// of it is that exec's. The agent serves concurrent execs and cannot tell
// whose such an orphan is, so it does not charge it and sweeps it after the
// exec instead.
package memwatch

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zinc-sig/ghost/internal/sandbox"
	"golang.org/x/sys/unix"
)

// sampleInterval is the cadence at which the sampler reads /proc. One read
// of every status file per tick is cheap; the page-table walk behind a
// Pss confirmation is not, which is why confirmations are rate limited.
const sampleInterval = 100 * time.Millisecond

// confirmHysteresisTicks is how many ticks the sampler waits after a
// confirmation found a group under budget before it confirms that group
// again. A fork-heavy harness whose resident sum overcounts copy-on-write
// pages would otherwise pay a smaps_rollup walk on every tick.
const confirmHysteresisTicks = 10

// Sampler enforces the per-exec memory budget for every running exec
// from one goroutine, attributes kernel out-of-memory kills to the exec
// that lost a process, and sweeps processes that escaped their exec's
// process group. Registration is by process group, so the child must be
// started with Setpgid. The goroutine starts with the first registered
// exec and exits when the last one finishes. The reasoning behind the
// budget, the confirmation, the attribution rule, and what the sampler
// cannot see is in the memory budgets section of the agent README.
type Sampler struct {
	mu       sync.Mutex
	procRoot string
	selfPid  int
	// readOOMKills returns the cgroup's oom_kill counter; 0 when the
	// counter is unreadable, which makes every comparison read as no kill.
	readOOMKills func() int64

	watches  map[int]*Watch
	running  bool
	oomKills int64
	oomKnown bool

	// subreaper is set by EnableSubreaper: this process is a child
	// subreaper serving exactly one exec, so every descendant of it is that
	// exec's (see EnableSubreaper).
	subreaper bool
	// reapStop ends the goroutine that reaps orphans on SIGCHLD, and
	// reapDone is closed when it has ended.
	reapStop chan struct{}
	reapDone chan struct{}
}

// maxSweepPasses bounds the final sweep of a subreaper: each pass kills
// the descendants one snapshot shows, and a process forking in a loop can
// add a child between the snapshot and the kill, so passes repeat until
// one finds nothing.
const maxSweepPasses = 50

// EnableSubreaper makes this process a child subreaper, so a descendant
// whose parent exits is reparented to it instead of to the container's
// init, and charges every descendant of it to the one registered exec. It
// is for a process that serves exactly one exec and is created for it
// (supervise): nothing but that exec can have put a process under it, so
// the attribution needs no guess, and an orphan in a group of its own (a
// double fork into a new session) stays charged and is swept. The agent
// must not use it: it serves concurrent execs and cannot tell whose an
// orphan is. Orphans that exit are reaped on SIGCHLD, as an init does,
// because a zombie counts against the task cap until it is reaped and a
// command that spawns short-lived detached processes quickly would
// otherwise exhaust the cap between two ticks. Finish ends the reaping.
func (s *Sampler) EnableSubreaper() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGCHLD)
	s.mu.Lock()
	s.subreaper = true
	s.reapStop = make(chan struct{})
	s.reapDone = make(chan struct{})
	s.mu.Unlock()
	go s.reapLoop(sigs)
	return nil
}

func (s *Sampler) reapLoop(sigs chan os.Signal) {
	defer close(s.reapDone)
	defer signal.Stop(sigs)
	for {
		select {
		case <-s.reapStop:
			return
		case <-sigs:
			s.reapExited()
		}
	}
}

// reapExited reaps exited children of this process until none is left or
// the next one is a registered exec's root, which is left for os/exec's
// Wait and is reaped by it at once; the tick reaps whatever that leaves.
func (s *Sampler) reapExited() {
	for {
		pid := peekExited()
		if pid <= 0 {
			return
		}
		s.mu.Lock()
		_, root := s.watches[pid]
		s.mu.Unlock()
		if root {
			return
		}
		var ws syscall.WaitStatus
		if got, _ := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil); got != pid {
			return
		}
	}
}

// stopReaper ends the SIGCHLD reaping goroutine, if one runs. It must be
// called without the lock held, since the goroutine takes it.
func (s *Sampler) stopReaper() {
	if s.reapStop == nil {
		return
	}
	close(s.reapStop)
	<-s.reapDone
	s.reapStop = nil
}

// Watch is the sampler's record of one registered exec. pgid is the root
// process's pid, which is also its group id.
type Watch struct {
	pgid   int
	budget int64
	peak   int64
	// prevSum and prevPids are the members as sampled at the previous
	// tick; kernel kill attribution compares the current tick against them.
	prevSum  int64
	prevPids map[int]struct{}
	// confirmSkip counts down the ticks left before the next Pss
	// confirmation after a negative one.
	confirmSkip int
	exceeded    bool
	// reason is "sampler" when the sampler killed the members past their
	// budget and "kernel" when a cgroup kill was attributed to them.
	reason string
}

// Outcome is what the caller records on the result for one exec.
type Outcome struct {
	Exceeded bool
	Reason   string
	Peak     int64
	// Swept lists the pids the orphan sweep killed after this exec.
	Swept []int
}

// attributionSample is one registered exec as seen by the attribution
// rule at the tick in which the oom_kill counter rose.
type attributionSample struct {
	lostPid bool
	lastSum int64
	budget  int64
}

func New() *Sampler {
	return &Sampler{
		procRoot:     "/proc",
		selfPid:      os.Getpid(),
		readOOMKills: sandbox.ReadOOMKillCount,
		watches:      make(map[int]*Watch),
	}
}

// StartWatched starts cmd and registers it under the sampler's lock, so a
// sweep triggered by a sibling exec cannot kill the child between the start
// and the registration. The child must be started with Setpgid. A budget
// of 0 registers the exec without enforcement; registration still protects
// its members from the sweep and still measures its peak.
func (s *Sampler) StartWatched(cmd *exec.Cmd, budget int64) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w := &Watch{pgid: cmd.Process.Pid, budget: budget}
	s.watches[w.pgid] = w
	if !s.running {
		s.running = true
		// The counter is re-based on the first tick of every run of the
		// loop, so a kill that happened while no exec was registered is
		// not attributed to the next one.
		s.oomKnown = false
		go s.loop()
	}
	return w, nil
}

func (s *Sampler) loop() {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		if len(s.watches) == 0 {
			s.running = false
			s.mu.Unlock()
			return
		}
		s.sampleLocked()
		s.mu.Unlock()
	}
}

// Finish takes one last sample so a kernel kill in the exec's final tick
// is still attributed, kills the members that sample finds (a descendant
// that left the group and outlived the root's wait), unregisters the exec,
// and sweeps every process that is not a member of a registered exec. The
// caller has already killed the exec's own group.
func (s *Sampler) Finish(w *Watch) Outcome {
	defer s.stopReaper()
	s.mu.Lock()
	defer s.mu.Unlock()
	members := s.sampleLocked()
	killMembers(w.pgid, members[w.pgid].pids)
	delete(s.watches, w.pgid)
	swept := s.sweepLocked()
	if s.subreaper {
		for pass := 1; pass < maxSweepPasses; pass++ {
			more := s.sweepLocked()
			if len(more) == 0 {
				break
			}
			swept = append(swept, more...)
		}
		reapAll()
	}
	return Outcome{
		Exceeded: w.exceeded,
		Reason:   w.reason,
		Peak:     w.peak,
		Swept:    swept,
	}
}

// sampleLocked runs one tick: kernel kill attribution against the previous
// tick, then the budget check for every registered exec. It returns the
// members of every registered exec keyed by the root's pid.
func (s *Sampler) sampleLocked() map[int]groupSample {
	kills := s.readOOMKills()
	samples, err := snapshotProcs(s.procRoot)
	if err != nil {
		// Without /proc there is nothing to enforce or attribute; the
		// peak stays 0.
		s.oomKills, s.oomKnown = kills, true
		return nil
	}
	children := childrenOf(samples)
	groups := make(map[int]groupSample, len(s.watches))
	for pgid := range s.watches {
		groups[pgid] = membersOf(pgid, samples, children)
		if s.subreaper && len(s.watches) == 1 {
			groups[pgid] = withDescendants(groups[pgid], s.selfPid, children)
		}
	}
	if s.subreaper {
		s.reapOrphansLocked(samples)
	}

	if s.oomKnown && kills > s.oomKills {
		s.attributeLocked(groups)
	}
	s.oomKills, s.oomKnown = kills, true

	for _, w := range s.watches {
		g := groups[w.pgid]
		if g.sum > w.peak {
			w.peak = g.sum
		}
		if w.budget > 0 && !w.exceeded && g.sum > w.budget {
			if w.confirmSkip > 0 {
				w.confirmSkip--
			} else if confirmedGroupBytes(s.procRoot, g.pids) > w.budget {
				w.exceeded, w.reason = true, "sampler"
				killMembers(w.pgid, g.pids)
			} else {
				w.confirmSkip = confirmHysteresisTicks
			}
		}
		w.prevSum = g.sum
		w.prevPids = make(map[int]struct{}, len(g.pids))
		for _, pid := range g.pids {
			w.prevPids[pid] = struct{}{}
		}
	}
	return groups
}

// attributeLocked flags the execs the attribution rule selects for a rise
// of the oom_kill counter and kills their members, so a kernel kill of a
// descendant ends the exec the same way a sampler kill does.
func (s *Sampler) attributeLocked(groups map[int]groupSample) {
	watches := make([]*Watch, 0, len(s.watches))
	samples := make([]attributionSample, 0, len(s.watches))
	for _, w := range s.watches {
		lost := false
		current := groups[w.pgid].pids
		for pid := range w.prevPids {
			if !containsPid(current, pid) {
				lost = true
				break
			}
		}
		watches = append(watches, w)
		samples = append(samples, attributionSample{lostPid: lost, lastSum: w.prevSum, budget: w.budget})
	}
	for _, i := range attributeKernelKill(samples) {
		w := watches[i]
		if w.exceeded {
			continue
		}
		w.exceeded, w.reason = true, "kernel"
		killMembers(w.pgid, groups[w.pgid].pids)
	}
}

// attributeKernelKill returns the indexes of the execs to flag when the
// cgroup's oom_kill counter rose during a tick. A candidate is an exec
// whose members lost a process in that tick. It is flagged when its last
// sampled sum was at or over its budget, or when it was the only
// registered exec. Any other candidate keeps its exit code unflagged,
// because the kernel may have chosen it for memory another exec, or the
// agent, allocated.
func attributeKernelKill(samples []attributionSample) []int {
	var flagged []int
	for i, c := range samples {
		if !c.lostPid {
			continue
		}
		if (c.budget > 0 && c.lastSum >= c.budget) || len(samples) == 1 {
			flagged = append(flagged, i)
		}
	}
	return flagged
}

// sweepLocked kills every live descendant of the agent that is not a
// member of a registered exec (see membersOf), and returns the pids it
// signalled. Descendants are found by walking parent pids, which reaches a
// process that left its group with setsid because such a process reparents
// to the agent when its parent exits. As pid 1 the agent is every process's
// ancestor, so this is every process in the container except the agent.
// Supervise is not pid 1 (it runs through the container runtime's exec);
// with EnableSubreaper an orphan still reparents to it, and without it the
// orphan reparents to the container's init and this sweep does not reach
// it.
func (s *Sampler) sweepLocked() []int {
	samples, err := snapshotProcs(s.procRoot)
	if err != nil {
		return nil
	}
	children := childrenOf(samples)
	protected := map[int]bool{}
	for pgid := range s.watches {
		for _, pid := range membersOf(pgid, samples, children).pids {
			protected[pid] = true
		}
	}
	var killed []int
	queue := []int{s.selfPid}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, p := range children[parent] {
			queue = append(queue, p.pid)
			if p.zombie {
				continue
			}
			if protected[p.pid] {
				continue
			}
			if _, registered := s.watches[p.pgid]; registered {
				continue
			}
			if err := syscall.Kill(p.pid, syscall.SIGKILL); err == nil {
				killed = append(killed, p.pid)
			}
		}
	}
	return killed
}

// reapOrphansLocked reaps every zombie child of this process that is not
// a registered exec's root: an orphan reparented to the subreaper that
// exited. A root is left for os/exec's Wait, which would otherwise see
// ECHILD, so reaping is by exact pid, never wait4(-1).
func (s *Sampler) reapOrphansLocked(samples []procSample) {
	for _, p := range samples {
		if p.ppid != s.selfPid || !p.zombie {
			continue
		}
		if _, root := s.watches[p.pid]; root {
			continue
		}
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(p.pid, &ws, syscall.WNOHANG, nil)
	}
}

// reapAll reaps every exited child. It runs only after the exec's root was
// waited for, so no one else is waiting on a child.
func reapAll() {
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return
		}
	}
}

// killMembers kills the exec's group, which also reaches a group member
// spawned after the snapshot, and every member pid, which reaches a
// descendant that left the group.
func killMembers(pgid int, pids []int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func containsPid(pids []int, pid int) bool {
	for _, p := range pids {
		if p == pid {
			return true
		}
	}
	return false
}
