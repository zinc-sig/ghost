package agent

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/automaxprocs/maxprocs"

	"github.com/zinc-sig/ghost/internal/agent/contract"
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
	// Pin GOMAXPROCS to the container's cgroup CPU quota (core backend/12
	// root-cause fix). Grading containers run with a 1-CPU bandwidth quota
	// while the node has ~16 cores; Go 1.24 sizes GOMAXPROCS from
	// runtime.NumCPU() (the quota does not lower nproc), so the runtime
	// carries 16 Ps' worth of scheduling and timer machinery on a sliver of
	// one CPU shared with CPU-pegging student processes. Under CFS bandwidth
	// throttling that starves the runtime's timer servicing: the Temporal
	// SDK's batched heartbeat-send timer (one wire send per ~48-60s window)
	// fires late, the send is never attempted inside the server's margin,
	// and a live container is declared dead.
	//
	// The ceil-rounding and floor-of-2 below reproduce Go 1.25+'s native
	// container-aware formula, min(NumCPU, max(ceil(quota), 2))
	// (runtime/cgroup_linux.go): GOMAXPROCS=2 for the 1-CPU grading quota.
	// Upstream floors at 2 for GC and runtime progress headroom, and
	// matching it keeps a future `go 1.25+` go.mod bump behavior-neutral.
	// The native mechanism is gated on the go.mod language version, not the
	// toolchain, and an explicit GOMAXPROCS pin disables its dynamic quota
	// tracking, so this call is not redundant until the go directive moves to
	// 1.25+ and this pin is removed with it. A pre-set GOMAXPROCS env var
	// (for example baked into a course environment image) takes precedence
	// over this pin; the boot line below exposes the effective value either
	// way. Failure to detect the quota is non-fatal: the agent logs and runs
	// with the default.
	if _, err := maxprocs.Set(
		maxprocs.Logger(func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, "ghost agent: "+format+"\n", args...)
		}),
		maxprocs.RoundQuotaFunc(func(q float64) int { return int(math.Ceil(q)) }),
		maxprocs.Min(2),
	); err != nil {
		fmt.Fprintf(os.Stderr, "ghost agent: maxprocs: %v (continuing with default GOMAXPROCS)\n", err)
	}
	// Boot forensics: one line that answers "what was the runtime actually
	// configured as" for every future run (core backend/12 taught us this
	// is the first question of any heartbeat investigation).
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
