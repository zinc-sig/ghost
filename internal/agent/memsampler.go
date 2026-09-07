package agent

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

// memorySampler enforces the per-exec memory budget for every running exec
// from one goroutine, attributes kernel out-of-memory kills to the exec
// that lost a process, and sweeps processes that escaped their exec's
// process group. Registration is by process group, so the child must be
// started with Setpgid. The goroutine starts with the first registered
// exec and exits when the last one finishes. The reasoning behind the
// budget, the confirmation, the attribution rule, and what the sampler
// cannot see is in the memory budgets section of README.md.
type memorySampler struct {
	mu       sync.Mutex
	procRoot string
	selfPid  int
	// readOOMKills returns the cgroup's oom_kill counter; 0 when the
	// counter is unreadable, which makes every comparison read as no kill.
	readOOMKills func() int64

	watches  map[int]*execWatch
	running  bool
	oomKills int64
	oomKnown bool
}

// execWatch is the sampler's record of one registered exec.
type execWatch struct {
	pgid   int
	budget int64
	peak   int64
	// prevSum and prevPids are the group as sampled at the previous tick;
	// kernel kill attribution compares the current tick against them.
	prevSum  int64
	prevPids map[int]struct{}
	// confirmSkip counts down the ticks left before the next Pss
	// confirmation after a negative one.
	confirmSkip int
	exceeded    bool
	// reason is "sampler" when the sampler killed the group past its
	// budget and "kernel" when a cgroup kill was attributed to it.
	reason string
}

// memoryOutcome is what RunExec records on the result for one exec.
type memoryOutcome struct {
	exceeded bool
	reason   string
	peak     int64
	// swept lists the pids the orphan sweep killed after this exec.
	swept []int
}

// attributionSample is one registered exec as seen by the attribution
// rule at the tick in which the oom_kill counter rose.
type attributionSample struct {
	lostPid bool
	lastSum int64
	budget  int64
}

func newMemorySampler() *memorySampler {
	return &memorySampler{
		procRoot:     "/proc",
		selfPid:      os.Getpid(),
		readOOMKills: sandbox.ReadOOMKillCount,
		watches:      make(map[int]*execWatch),
	}
}

// startWatched starts cmd and registers its process group under the
// sampler's lock, so a sweep triggered by a sibling exec cannot kill the
// child between the start and the registration. A budget of 0 registers
// the exec without enforcement; registration still protects the group
// from the sweep and still measures its peak.
func (s *memorySampler) startWatched(cmd *exec.Cmd, budget int64) (*execWatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w := &execWatch{pgid: cmd.Process.Pid, budget: budget}
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

func (s *memorySampler) loop() {
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

// finish takes one last sample so a kernel kill in the exec's final tick
// is still attributed, unregisters the exec, and sweeps every process that
// is not in a registered group. The caller has already killed the exec's
// own group.
func (s *memorySampler) finish(w *execWatch) memoryOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleLocked()
	delete(s.watches, w.pgid)
	return memoryOutcome{
		exceeded: w.exceeded,
		reason:   w.reason,
		peak:     w.peak,
		swept:    s.sweepLocked(),
	}
}

// sampleLocked runs one tick: kernel kill attribution against the previous
// tick, then the budget check for every registered exec.
func (s *memorySampler) sampleLocked() {
	kills := s.readOOMKills()
	samples, err := snapshotProcs(s.procRoot)
	if err != nil {
		// Without /proc there is nothing to enforce or attribute; the
		// peak stays 0.
		s.oomKills, s.oomKnown = kills, true
		return
	}
	groups := groupByPgid(samples)

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
				killGroup(w.pgid)
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
}

// attributeLocked flags the execs the attribution rule selects for a rise
// of the oom_kill counter and kills their groups, so a kernel kill of a
// descendant ends the exec the same way a sampler kill does.
func (s *memorySampler) attributeLocked(groups map[int]groupSample) {
	watches := make([]*execWatch, 0, len(s.watches))
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
		killGroup(w.pgid)
	}
}

// attributeKernelKill returns the indexes of the execs to flag when the
// cgroup's oom_kill counter rose during a tick. A candidate is an exec
// whose group lost a process in that tick. It is flagged when its last
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

// sweepLocked kills every live descendant of the agent whose process group
// is not a registered exec, and returns the pids it signalled. Descendants
// are found by walking parent pids, which reaches a process that left its
// group with setsid because such a process reparents to the agent when its
// parent exits. As pid 1 the agent is every process's ancestor, so this is
// every process in the container except the agent.
func (s *memorySampler) sweepLocked() []int {
	samples, err := snapshotProcs(s.procRoot)
	if err != nil {
		return nil
	}
	children := make(map[int][]procSample)
	for _, p := range samples {
		children[p.ppid] = append(children[p.ppid], p)
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

func killGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func containsPid(pids []int, pid int) bool {
	for _, p := range pids {
		if p == pid {
			return true
		}
	}
	return false
}
