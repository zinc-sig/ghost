// Package agent implements ghost's agent mode (RFD 0015): a long-lived
// Temporal worker inside a grading container that joins a per-run task
// queue, serves exactly two activities (fetch-submission and run-exec),
// and runs each command in a sandboxed child process. The agent itself is
// never sandboxed. Landlock and RLIMIT_NPROC are process-wide and
// irreversible, so the child (`ghost exec`) applies them just before
// execve.
package agent

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// agentEnvPrefix is the prefix of every agent boot/config environment
// variable. The contract requires the agent to scrub ALL variables with
// this prefix from the environment student commands run with.
const agentEnvPrefix = "GHOST_AGENT_"

// Agent-internal knobs (not part of the frozen contract; they share the
// GHOST_AGENT_ prefix so they are scrubbed alongside the credentials).
const (
	// EnvStagingDir overrides the agent-owned staging directory used for
	// stdin materialisation and stdio capture files. Defaults to a fresh
	// 0700 temp directory.
	EnvStagingDir = "GHOST_AGENT_STAGING_DIR"
	// EnvDefaultTimeout is the exec timeout applied when
	// ExecSpec.TimeoutMs is 0, as a Go duration string (default "60s").
	EnvDefaultTimeout = "GHOST_AGENT_DEFAULT_TIMEOUT"
	// EnvMaxPids is the RLIMIT_NPROC value the child applies before
	// execve (default 32; 0 disables the limit).
	EnvMaxPids = "GHOST_AGENT_MAX_PIDS"
	// EnvDefaultOutputLimit is the per-file output cap (bytes,
	// RLIMIT_FSIZE in the child) applied when ExecSpec.OutputLimitBytes
	// is 0. Default 64 MiB. Bake-time tunable per image; setting it to 0
	// disables the default entirely, as an emergency escape hatch. The
	// platform ships with the cap on because a 341-byte grader-bomb
	// stored 997MB of stdout through the uncapped path (core
	// backend/12).
	EnvDefaultOutputLimit = "GHOST_AGENT_DEFAULT_OUTPUT_LIMIT"
	// EnvSandbox toggles Landlock filesystem sandboxing of the child
	// (default true; only disabled in test environments where Landlock
	// is unavailable).
	EnvSandbox = "GHOST_AGENT_SANDBOX"
)

// defaultMaxConcurrentExecs bounds concurrent activity execution per
// container. 4 keeps the live process count well under the default
// MaxPids (32) even for multi-process commands, while still overlapping
// scenarios for throughput.
const defaultMaxConcurrentExecs = 4

// Config is the agent's boot configuration, read from the GHOST_AGENT_*
// environment (see contract.Env* and the agent-internal Env* consts).
type Config struct {
	// Temporal connection (contract).
	TemporalAddress   string
	TemporalNamespace string
	TaskQueue         string
	// AuthToken is empty in the trusted-network interim; it is required
	// once the per-run-queue token authorizer ships.
	AuthToken string

	// Object storage (contract).
	StorageEndpoint     string
	StorageAccessKey    string
	StorageSecretKey    string
	StorageSessionToken string
	StorageSecure       bool

	// Workdir is the run workspace root all relative paths resolve
	// against.
	Workdir string

	// Agent-internal knobs.
	StagingDir     string
	DefaultTimeout time.Duration
	// DefaultOutputLimit is the per-file output cap (bytes) applied when
	// ExecSpec.OutputLimitBytes is 0; 0 disables the default.
	DefaultOutputLimit int64
	MaxPids            uint64
	Sandbox            bool

	// MaxConcurrentExecs bounds how many activities run at once in this
	// container (worker.Options.MaxConcurrentActivityExecutionSize).
	MaxConcurrentExecs int

	// GhostPath is the ghost binary spawned as the sandboxed child
	// (`ghost exec ...`). Defaults to the running executable;
	// overridden in tests.
	GhostPath string

	// AgentVersion is the ghost build version reported in
	// FetchSubmissionResult (diagnostics only).
	AgentVersion string
}

// LoadConfig reads the agent configuration from the environment.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		TemporalNamespace:   getenvDefault(contract.EnvTemporalNamespace, "default"),
		AuthToken:           os.Getenv(contract.EnvTemporalAuthToken),
		StorageSessionToken: os.Getenv(contract.EnvStorageSessionToken),
		Workdir:             getenvDefault(contract.EnvWorkdir, "/workspace"),
		AgentVersion:        "dev",
	}

	var err error
	if cfg.TemporalAddress, err = requireEnv(contract.EnvTemporalAddress); err != nil {
		return nil, err
	}
	if cfg.TaskQueue, err = requireEnv(contract.EnvTaskQueue); err != nil {
		return nil, err
	}
	if cfg.StorageEndpoint, err = requireEnv(contract.EnvStorageEndpoint); err != nil {
		return nil, err
	}
	if cfg.StorageAccessKey, err = requireEnv(contract.EnvStorageAccessKey); err != nil {
		return nil, err
	}
	if cfg.StorageSecretKey, err = requireEnv(contract.EnvStorageSecretKey); err != nil {
		return nil, err
	}

	if cfg.StorageSecure, err = parseBoolEnv(contract.EnvStorageSecure, false); err != nil {
		return nil, err
	}
	if cfg.Sandbox, err = parseBoolEnv(EnvSandbox, true); err != nil {
		return nil, err
	}

	if v := os.Getenv(EnvDefaultTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid %s %q: %w", EnvDefaultTimeout, v, err)
		}
		cfg.DefaultTimeout = d
	} else {
		cfg.DefaultTimeout = 60 * time.Second
	}

	if v := os.Getenv(EnvDefaultOutputLimit); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("agent: invalid %s %q: want a non-negative byte count", EnvDefaultOutputLimit, v)
		}
		cfg.DefaultOutputLimit = n
	} else {
		cfg.DefaultOutputLimit = 64 << 20 // 64 MiB
	}

	if v := os.Getenv(EnvMaxPids); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid %s %q: %w", EnvMaxPids, v, err)
		}
		cfg.MaxPids = n
	} else {
		cfg.MaxPids = 32
	}

	if v := os.Getenv(contract.EnvMaxConcurrentExecs); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("agent: invalid %s %q: want a non-negative integer", contract.EnvMaxConcurrentExecs, v)
		}
		cfg.MaxConcurrentExecs = n
	}
	if cfg.MaxConcurrentExecs <= 0 {
		cfg.MaxConcurrentExecs = defaultMaxConcurrentExecs
	}

	// Staging is agent-owned (stdin materialisation, stdio captures) and
	// must not be world-writable: 0700, never a shared /tmp path the
	// sandboxed student process could scribble over.
	if dir := os.Getenv(EnvStagingDir); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("agent: failed to create staging dir %s: %w", dir, err)
		}
		cfg.StagingDir = dir
	} else {
		dir, err := os.MkdirTemp("", "ghost-agent-staging-")
		if err != nil {
			return nil, fmt.Errorf("agent: failed to create staging dir: %w", err)
		}
		cfg.StagingDir = dir
	}

	if cfg.GhostPath, err = os.Executable(); err != nil {
		return nil, fmt.Errorf("agent: failed to resolve own executable: %w", err)
	}

	return cfg, nil
}

func requireEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("agent: required environment variable %s is not set", name)
	}
	return v, nil
}

func getenvDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func parseBoolEnv(name string, def bool) (bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("agent: invalid %s %q: %w", name, v, err)
	}
	return b, nil
}
