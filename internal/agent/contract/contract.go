// Package contract is the FROZEN wire contract between core's grading
// workflows and the ghost agent (RFD 0015 Decision 3: the command
// channel is Temporal; the agent is a worker on a per-run task queue and
// each command is an activity).
//
// This file is a duplicated-but-pinned copy of the core repo's
// internal/pipeline/agentcontract/contract.go (RFD 0015): the golden JSON
// fixtures under testdata/ are byte-identical across the two repos and pin
// the encoding (Temporal's default data converter marshals activity
// payloads as JSON, so the json tags below are the wire format). Editing
// this file requires the same edit to the core copy, the same update to
// both repos' testdata goldens, and a ProtocolVersion bump for any change
// that is not strictly additive-and-optional.
package contract

import "time"

// ProtocolVersion is the newest protocol this contract describes and is
// carried on every activity input. The agent accepts any version from
// MinProtocolVersion to its own ProtocolVersion and echoes the version it
// was sent; outside that range it fails the activity with a non-retryable
// ApplicationError of type ProtocolMismatchErrorType, so a stale agent
// surfaces as "agent too old, rebuild the environment image" at the
// readiness handshake instead of as a payload decode error mid-run.
//
// Version 2 requires memory-budget enforcement and the concurrency setting
// in EnvMaxConcurrentExecs, which the container memory cap depends on.
// Version 3 adds FetchSubmissionInput.Objects and FetchSubmissionInput.Answer;
// an input carrying either at version 2 is a mismatch. Core sends 3 only for
// a run that uses them and 2 otherwise, so an agent built for 3 serves a
// core still sending 2.
const ProtocolVersion = 3

// MinProtocolVersion is the oldest protocol the agent still serves. Core
// sends it for a run that uses no version-3 field.
const MinProtocolVersion = 2

// ProtocolMismatchErrorType is the Temporal ApplicationError type the
// agent uses for version-skew failures. Core treats it as terminal for
// the run (non-retryable): retrying cannot fix a stale image.
const ProtocolMismatchErrorType = "GhostProtocolMismatch"

// StagingInvalidErrorType is the Temporal ApplicationError type the agent
// uses when the workspace cannot be staged as instructed: a teacher object
// wrote a directory at the answer's target path or a file where one of its
// parent directories must be. Core treats it as a configuration failure
// (not retried): the marking-scheme assets and the submission path clash,
// and rerunning cannot fix that. Nothing the student delivers raises it: a
// delivered file or directory in the answer's way is removed, and several
// root files matching the stem are resolved by name order.
const StagingInvalidErrorType = "GhostStagingInvalid"

// Activity names the agent registers on its per-run task queue and the
// PipelineRunWorkflow schedules by name.
const (
	// FetchSubmissionActivity is always the run's first activity. Its
	// schedule-to-start timeout doubles as the readiness gate (an agent
	// that never connects never starts it), and its result carries the
	// agent's protocol version for the skew check.
	FetchSubmissionActivity = "ghost-fetch-submission"
	// RunExecActivity runs exactly one resolved exec spec per invocation,
	// a single (stage, scenario) pair, never a batch (RFD 0015 Decision
	// 1: per-exec command granularity).
	RunExecActivity = "ghost-run-exec"
)

// Agent boot configuration environment variables, injected by the
// runner backend at dispatch. The names are part of this contract; the
// delivery mechanism (plain env vs file vs secret mount) is the v1
// interim and may be hardened (RFD 0015 Decision 8) without renaming.
// The agent must read these at boot and scrub them from the
// environment it spawns student commands with.
const (
	EnvTemporalAddress   = "GHOST_AGENT_TEMPORAL_ADDRESS"
	EnvTemporalNamespace = "GHOST_AGENT_TEMPORAL_NAMESPACE"
	EnvTaskQueue         = "GHOST_AGENT_TASK_QUEUE"
	// EnvTemporalAuthToken is empty in the trusted-network interim; once
	// the per-run-queue token authorizer ships, it is required.
	EnvTemporalAuthToken = "GHOST_AGENT_TEMPORAL_AUTH_TOKEN"

	EnvStorageEndpoint  = "GHOST_AGENT_STORAGE_ENDPOINT"
	EnvStorageAccessKey = "GHOST_AGENT_STORAGE_ACCESS_KEY"
	EnvStorageSecretKey = "GHOST_AGENT_STORAGE_SECRET_KEY"
	// EnvStorageSessionToken is empty with static interim credentials; it
	// is populated once per-run STS credentials ship.
	EnvStorageSessionToken = "GHOST_AGENT_STORAGE_SESSION_TOKEN"
	EnvStorageSecure       = "GHOST_AGENT_STORAGE_SECURE"

	// EnvWorkdir is the run workspace root all relative paths resolve
	// against (downloads land here; ExecSpec.Workdir is relative to it).
	EnvWorkdir = "GHOST_AGENT_WORKDIR"

	// EnvMaxConcurrentExecs bounds how many run-exec activities the agent
	// serves at once. Core sets it at dispatch and sizes the container's
	// memory cap from the same number, because every concurrently running
	// exec's memory budget must fit under the cap for the per-exec budget
	// to bind before the cap does. An unset or non-positive value leaves
	// the agent's own default in effect.
	EnvMaxConcurrentExecs = "GHOST_AGENT_MAX_CONCURRENT_EXECS"
)

// FetchSubmissionInput asks the agent to download the run's inputs (the
// student submission and the derived/config assets) into the run
// workspace (RFD 0015 Decision 7: the agent fetches its own inputs).
//
// The agent stages the workspace in this order, on every attempt from
// scratch: every Downloads entry in list order, then the answer is set
// aside (see AnswerSpec), then every Objects entry in list order, then the
// answer is placed. A later write replaces whatever an earlier one left at
// the same path, so a teacher object overwrites a student-delivered file.
// Objects and Answer require protocol 3; a version-2 input carries
// neither.
type FetchSubmissionInput struct {
	ProtocolVersion int            `json:"protocol_version"`
	Downloads       []DownloadSpec `json:"downloads"`
	// Objects are single teacher-owned files written to one or more
	// workspace paths after the prefix downloads.
	Objects []ObjectSpec `json:"objects,omitempty"`
	// Answer names the student's answer file and where it ends up. Present
	// whenever the run carries Objects or a submission path, so the answer
	// survives the object overlay.
	Answer *AnswerSpec `json:"answer,omitempty"`
}

// DownloadSpec is one object-storage prefix to mirror into the
// workspace. The agent must apply path-traversal defence: an object key
// under Prefix must never produce a file outside TargetDir (reject
// keys whose relative path escapes, e.g. via "..").
type DownloadSpec struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	// TargetDir is relative to the run workspace root; "." for the root.
	TargetDir string `json:"target_dir"`
}

// ObjectSpec is one object-storage key written to every TargetPaths entry.
// Each target is relative to the run workspace root and must resolve
// inside it; the agent creates missing parent directories, and a file
// where a parent directory must be or a directory at the target is
// removed before the write. A missing key fails the activity before any
// target is created.
type ObjectSpec struct {
	Bucket      string   `json:"bucket"`
	Key         string   `json:"key"`
	TargetPaths []string `json:"target_paths"`
}

// AnswerSpec identifies the student's answer file among the downloaded
// root files and says where the agent places it once the objects are
// written. The answer is the regular file at the workspace root whose name
// minus its extension equals Stem, read from the files the current attempt
// downloaded, never from an earlier attempt. No match is a no-op (a teacher
// stub at the target, if any, stays); when several root files match, the
// first by byte-wise name order is the answer and the others stay where
// they are, because only the student's delivery can produce that shape.
// The answer is placed last and always wins its target: a teacher object
// at the same path is replaced, and a student-delivered file or directory
// in the way is removed. A directory written there by an object, or an
// object file where a parent directory of the target must be, is
// StagingInvalidErrorType.
type AnswerSpec struct {
	Stem string `json:"stem"`
	// TargetPath is relative to the run workspace root. Empty means the
	// answer is restored under its original root name after the objects
	// are written, so a teacher object with that name cannot replace it.
	TargetPath string `json:"target_path"`
}

// FetchSubmissionResult reports the download outcome and completes the
// readiness/version handshake.
type FetchSubmissionResult struct {
	AgentProtocolVersion int `json:"agent_protocol_version"`
	// AgentVersion is the ghost build version (diagnostics only; the
	// protocol version is what gates compatibility).
	AgentVersion string `json:"agent_version"`
	Files        int    `json:"files"`
	Bytes        int64  `json:"bytes"`
}

// RunExecInput is one resolved exec command. The workflow schedules one
// per (stage, scenario) on the per-run queue; stage/scenario_code are
// echoed for the agent's logging/tracing only. The workflow keys the
// result by the activity it scheduled, and dependency gating (skipped)
// is entirely the workflow's concern.
type RunExecInput struct {
	ProtocolVersion int             `json:"protocol_version"`
	Stage           string          `json:"stage"`
	ScenarioCode    string          `json:"scenario_code"`
	Spec            ExecSpec        `json:"spec"`
	StdioUpload     StdioUploadSpec `json:"stdio_upload"`
}

// StdioUploadSpec tells the agent where to upload the captured stdio.
// Keys are KeyPrefix + "/stdin" | "/stdout" | "/stderr"; the resulting
// URIs (see URIFor) are opaque handles to everything downstream.
type StdioUploadSpec struct {
	Bucket    string `json:"bucket"`
	KeyPrefix string `json:"key_prefix"`
}

// ExecSpec is the wire form of one scenario's command after RFD 0013
// inheritance/merge (the unit of work of RFD 0015 § Terminology).
//
// Semantics the agent must honour:
//   - No shell: Command is the program, Args the argument vector.
//   - Stdin: StdinContent is an inline literal the agent materialises
//     (and uploads as the stdin artifact); StdinPath is a workspace-
//     relative (or absolute, e.g. /dev/null) file. At most one is set.
//   - StdoutPath/StderrPath are *file write* destinations relative to
//     the effective workdir, for later stages to consume. Stream capture
//     to object storage happens unconditionally regardless of these.
//   - Workdir is relative to the run workspace root ("." = root).
//   - Env is overlaid on the agent's *scrubbed* base environment. The
//     student process must never inherit the GHOST_AGENT_* credentials.
type ExecSpec struct {
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	StdinContent *string  `json:"stdin_content,omitempty"`
	StdinPath    *string  `json:"stdin_path,omitempty"`
	StdoutPath   *string  `json:"stdout_path,omitempty"`
	StderrPath   *string  `json:"stderr_path,omitempty"`
	// TimeoutMs of 0 means the runtime default applies (agent-side).
	TimeoutMs int64 `json:"timeout_ms"`
	// OutputLimitBytes caps every file the command (and its descendants)
	// writes, per file (stdio captures included), via RLIMIT_FSIZE in the
	// child; breach kills the writer with SIGXFSZ and the exec is flagged
	// (ExecResult.OutputLimitExceeded), mirroring the timeout's
	// kill-and-flag semantics. Deliberately per-file, unlike supervise's
	// total-budget --max-output-bytes. 0 means the runtime default applies
	// (agent-side, GHOST_AGENT_DEFAULT_OUTPUT_LIMIT).
	OutputLimitBytes int64 `json:"output_limit_bytes"`
	// MemoryLimitBytes budgets the anonymous memory of the exec's process
	// group. The agent samples the group and kills it when the sum passes
	// the budget, flagging ExecResult.MemoryLimitExceeded; the container's
	// cgroup cap backstops what sampling misses, and the agent attributes
	// a kernel kill at the cap to the exec as well. Core sends the
	// effective budget on every exec. 0 means no per-exec budget, so only
	// the container cap applies; unlike OutputLimitBytes, 0 selects no
	// agent-side default, because an agent that enforced a default of its
	// own under a core that sizes the container without one would kill
	// execs the container had room for.
	MemoryLimitBytes int64             `json:"memory_limit_bytes"`
	Env              map[string]string `json:"env,omitempty"`
	Workdir          string            `json:"workdir"`
}

// ExecResult carries the RFD 0013 exec_result fields the agent can know
// on its own. The workflow owns the rest: `skipped` (dependency gating)
// and the derived per-scenario/stage state. The empty string on
// Error/URIs encodes the contract's null, matching the persistence layer.
type ExecResult struct {
	// Command/Args echo the resolved values actually executed.
	Command string   `json:"command"`
	Args    []string `json:"args"`
	// ExitCode is null only when the process could not be spawned (in
	// which case Error explains why).
	ExitCode *int `json:"exit_code"`
	// TimedOut reports that the agent killed the process for exceeding
	// its timeout (the exec still produces a result, not an error).
	TimedOut bool `json:"timed_out"`
	// OutputLimitExceeded reports that the exec hit its per-file output
	// limit (ExecSpec.OutputLimitBytes / the agent default): either the
	// kernel killed the direct child with SIGXFSZ, or a stdio capture
	// reached the limit even though the child exited normally (a
	// descendant took the signal, or a handler swallowed it and writes
	// failed with EFBIG). Like TimedOut, the exec still produces a
	// result, not an error.
	OutputLimitExceeded bool `json:"output_limit_exceeded"`
	// OutputLimitBytes echoes the per-file limit that was enforced for
	// this exec (spec value or agent default), so the flag above is
	// readable without the config at hand. 0 = no limit was enforced.
	OutputLimitBytes int64 `json:"output_limit_bytes"`
	// StdoutBytes/StderrBytes are the captured sizes on disk after the
	// exec (before upload). With OutputLimitExceeded set they show which
	// stream hit the cap; on normal runs they are grader-facing metadata.
	StdoutBytes int64 `json:"stdout_bytes"`
	StderrBytes int64 `json:"stderr_bytes"`
	// MemoryLimitExceeded reports that the exec was ended for memory:
	// the agent killed the process group past ExecSpec.MemoryLimitBytes,
	// or the kernel killed a process in the group at the container cap
	// and the agent attributed that kill from the cgroup's oom_kill
	// counter. Like TimedOut, it is a result, not an error.
	MemoryLimitExceeded bool `json:"memory_limit_exceeded"`
	// MemoryLimitBytes echoes the budget enforced for this exec, so the
	// flag above is readable without the config at hand. 0 = no per-exec
	// budget was enforced.
	MemoryLimitBytes int64 `json:"memory_limit_bytes"`
	// PeakMemoryBytes is the largest sampled anonymous-memory sum of the
	// exec's process group. 0 = not measured.
	PeakMemoryBytes int64 `json:"peak_memory_bytes"`
	// Error is a human-readable infra-level failure (spawn error,
	// upload failure, and so on). The empty string means none. A non-zero
	// exit is not an error.
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMs int64     `json:"duration_ms"`
	// Stdio URIs: always populated for stdout/stderr on an executed
	// command (zero-byte object for empty output); StdinURI populated
	// iff stdin was provided. The empty string means not captured.
	StdinURI  string `json:"stdin_uri,omitempty"`
	StdoutURI string `json:"stdout_uri,omitempty"`
	StderrURI string `json:"stderr_uri,omitempty"`
}

// URIFor is the frozen stdio-URI format: s3://<bucket>/<key>. The URI is
// an opaque dereferenceable handle: only core's artifact-serving layer
// ever parses it back.
func URIFor(bucket, key string) string {
	return "s3://" + bucket + "/" + key
}
