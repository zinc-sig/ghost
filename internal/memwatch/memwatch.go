// Package memwatch enforces per-exec memory budgets: a sampler reads /proc,
// kills a watched exec past its budget, attributes kernel out-of-memory
// kills, and sweeps escapees. The agent watches every concurrent exec with
// it and supervise watches its one child, so a sandbox Run and a graded run
// apply the same budget rule to the same processes.
//
// An exec's members are its root process, every descendant of the root,
// and every process in the root's group, recomputed on every tick. A child
// that leaves the group with setsid or setpgid is still a descendant and
// stays budgeted while its parent chain to the root lives. The one process
// the sampler cannot charge is an orphan in a group of its own: a
// descendant whose parent exited (it is reparented to the agent, or under
// supervise to the container's init) and that also left the exec's group.
// Only the agent, as pid 1, sweeps such an orphan after the exec; under
// supervise it is bounded by the container's memory cap until core resets
// the executor. Orphans are never charged to an exec by guesswork, because
// under supervise a leftover of an earlier exec would then count against
// the next one.
package memwatch

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/zinc-sig/ghost/internal/sandbox"
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
	s.mu.Lock()
	defer s.mu.Unlock()
	members := s.sampleLocked()
	killMembers(w.pgid, members[w.pgid].pids)
	delete(s.watches, w.pgid)
	return Outcome{
		Exceeded: w.exceeded,
		Reason:   w.reason,
		Peak:     w.peak,
		Swept:    s.sweepLocked(),
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
// Supervise is not pid 1 (it runs through the container runtime's exec), so
// an orphan reparents to the container's init instead and this sweep does
// not reach it.
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
