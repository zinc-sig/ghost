package agent

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/zinc-sig/ghost/internal/agent/contract"
	"github.com/zinc-sig/ghost/internal/cpuquota"
)

// agentMemoryLimit is the Go soft memory limit the agent runs under when
// GOMEMLIMIT is not set. The agent shares the container's memory cap with
// the student processes, and core sizes the cap with 256 MiB of headroom
// for the agent: this limit plus the 64 MiB shared-memory tmpfs. Within
// the limit sit the agent's baseline heap and one 16 MiB upload buffer per
// concurrent exec.
const agentMemoryLimit = 192 << 20

// Run connects to Temporal, joins the per-run task queue and serves the
// two contract activities until interrupted (SIGTERM/SIGINT drain the
// worker gracefully). The agent runs under a Go soft memory limit of
// agentMemoryLimit unless GOMEMLIMIT is set, so its own heap stays inside
// the headroom core adds to the container cap for it; a larger heap would
// let the agent's garbage collector defer collection until the cap, where
// the kernel kills a student process. The memory budgets section of
// README.md explains the layers.
func Run(cfg *Config) error {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(agentMemoryLimit)
	}
	// Orphans of exec children reparent to the agent so the sweep after
	// each exec finds them; as pid 1 this is already the case.
	if err := enableChildSubreaper(); err != nil {
		fmt.Fprintf(os.Stderr, "ghost agent: child subreaper: %v (continuing; escaped processes are swept only as pid 1)\n", err)
	}
	// The agent's environment carries the run credentials. Running
	// non-dumpable makes the agent's /proc/<pid>/environ, maps, and mem
	// unreadable by the same-UID student processes, which can read /proc
	// under Landlock. execve resets the flag, so exec children and the
	// sampler's reads of their /proc entries are unaffected. Without this
	// the credentials would be exposed, so a failure ends the run.
	if err := disableDumpable(); err != nil {
		return fmt.Errorf("agent: set non-dumpable: %w", err)
	}
	// The Go runtime's thread count follows the container's CPU quota rather
	// than the node's core count. With the default, a throttled grading
	// container (a 1-CPU quota on a node with many cores) starves the timer
	// that sends the Temporal heartbeat and a live container is declared
	// dead; cpuquota.PinGOMAXPROCS has the formula. An unreadable quota is
	// non-fatal: the agent logs and runs with the default.
	if err := cpuquota.PinGOMAXPROCS(func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "ghost agent: "+format+"\n", args...)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "ghost agent: maxprocs: %v (continuing with default GOMAXPROCS)\n", err)
	}
	// One line recording the effective runtime configuration, the first
	// question in any heartbeat investigation.
	fmt.Fprintf(os.Stderr, "ghost agent: runtime GOMAXPROCS=%d NumCPU=%d\n",
		runtime.GOMAXPROCS(0), runtime.NumCPU())

	if err := os.MkdirAll(cfg.Workdir, 0o755); err != nil {
		return fmt.Errorf("agent: failed to create workspace %s: %w", cfg.Workdir, err)
	}

	store, err := newObjectStore(cfg)
	if err != nil {
		return err
	}

	if cfg.Sandbox {
		// Operational posture (RFD 0015 Decision 9/10): ghost does not
		// isolate the network. The student command shares this container's
		// network namespace (including the egress the agent needs for
		// Temporal/object storage), so egress must be restricted by the
		// container/cluster via NetworkMode/NetworkPolicy (deny-egress
		// except the required endpoints).
		fmt.Fprintln(os.Stderr, "ghost agent: ghost does not isolate the network; the container/cluster must restrict egress via NetworkMode/NetworkPolicy (deny-egress except Temporal/object storage)")
	}

	opts := client.Options{
		HostPort:  cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	}
	if cfg.AuthToken != "" {
		// This is a seam for future per-run auth (RFD 0015 Decision 8): once
		// core issues per-run queue tokens, wire cfg.AuthToken into
		// opts.HeadersProvider ("authorization: Bearer <token>") and enable
		// TLS via opts.ConnectionOptions. The trusted-network interim
		// ignores it.
		_ = cfg.AuthToken
	}

	c, err := client.Dial(opts)
	if err != nil {
		return fmt.Errorf("agent: failed to connect to temporal at %s: %w", cfg.TemporalAddress, err)
	}
	defer c.Close()

	// Bound activity concurrency per container. Core dispatches all of a
	// stage's scenarios in parallel; without a cap a wide stage spawns N
	// sandboxed children at once and the concurrent process count can
	// exceed RLIMIT_NPROC (cfg.MaxPids), making fork/spawn fail
	// intermittently ("could not spawn" -> error scenario state).
	w := worker.New(c, cfg.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: cfg.MaxConcurrentExecs,
	})
	acts := NewActivities(cfg, store)
	w.RegisterActivityWithOptions(acts.FetchSubmission, activity.RegisterOptions{Name: contract.FetchSubmissionActivity})
	w.RegisterActivityWithOptions(acts.RunExec, activity.RegisterOptions{Name: contract.RunExecActivity})

	if err := w.Run(worker.InterruptCh()); err != nil {
		return fmt.Errorf("agent: worker stopped: %w", err)
	}
	return nil
}
