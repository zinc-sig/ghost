package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/zinc-sig/ghost/internal/agent/contract"
	"github.com/zinc-sig/ghost/internal/reaper"
)

// heartbeatInterval is how often the agent records an activity heartbeat
// while a child command is running. NOTE the wire reality (core backend/12):
// the Temporal SDK BATCHES these. The server actually receives one heartbeat
// per min(0.8×HeartbeatTimeout, 60s) window; this tick only guarantees a
// fresh heartbeat is queued for every send window. A var, not a const, so
// the upload-phase heartbeat regression test can shrink it.
var heartbeatInterval = 10 * time.Second

// heartbeatTickDelayWarn is the tick-servicing delay above which the agent
// warns about runtime CPU starvation. Normal servicing is µs–ms; delays of
// seconds mean the Go runtime is not getting CPU onto its timers (the core
// backend/12 pathology: at its worst, this delays the SDK's batched SEND
// timer past the server's margin, and a live container is declared dead).
// After the HeartbeatTimeout widening those stalls no longer fail runs, so
// this WARN is the surviving observability for the failure class: it turns
// every starvation event into a log line instead of silence.
const heartbeatTickDelayWarn = 3 * time.Second

// Activities implements the two contract activities the agent registers
// on its per-run task queue.
type Activities struct {
	cfg   *Config
	store ObjectStore
}

// NewActivities builds the activity implementations from the agent
// config and an object store (a fake in tests).
func NewActivities(cfg *Config, store ObjectStore) *Activities {
	return &Activities{cfg: cfg, store: store}
}

// checkProtocol fails the activity with a non-retryable ApplicationError
// of the contract's mismatch type on any version difference: retrying
// cannot fix a stale environment image.
func checkProtocol(version int) error {
	if version != contract.ProtocolVersion {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("agent protocol v%d, core requires v%d — rebuild the environment image",
				contract.ProtocolVersion, version),
			contract.ProtocolMismatchErrorType,
			nil,
		)
	}
	return nil
}

// FetchSubmission downloads the run's inputs into the workspace and
// completes the readiness/version handshake (contract:
// ghost-fetch-submission).
func (a *Activities) FetchSubmission(ctx context.Context, in contract.FetchSubmissionInput) (contract.FetchSubmissionResult, error) {
	if err := checkProtocol(in.ProtocolVersion); err != nil {
		return contract.FetchSubmissionResult{}, err
	}

	res := contract.FetchSubmissionResult{
		AgentProtocolVersion: contract.ProtocolVersion,
		AgentVersion:         a.cfg.AgentVersion,
	}

	for _, d := range in.Downloads {
		target, err := securePathUnder(a.cfg.Workdir, a.cfg.Workdir, d.TargetDir)
		if err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("download target %q: %w", d.TargetDir, err)
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("failed to create download target %s: %w", target, err)
		}
		files, bytes, err := a.store.DownloadPrefix(ctx, d.Bucket, d.Prefix, target)
		res.Files += files
		res.Bytes += bytes
		if err != nil {
			return contract.FetchSubmissionResult{}, err
		}
		activity.RecordHeartbeat(ctx)
	}
	return res, nil
}

// RunExec runs exactly one resolved exec spec in a sandboxed child
// process (contract: ghost-run-exec). Infra failures inside the exec
// (spawn errors, upload errors) are reported on the result's Error
// field, not as activity errors: the exec_result is the contract.
func (a *Activities) RunExec(ctx context.Context, in contract.RunExecInput) (contract.ExecResult, error) {
	if err := checkProtocol(in.ProtocolVersion); err != nil {
		return contract.ExecResult{}, err
	}

	logger := activity.GetLogger(ctx)
	logger.Info("run-exec", "stage", in.Stage, "scenario", in.ScenarioCode, "command", in.Spec.Command)

	spec := in.Spec
	res := contract.ExecResult{Command: spec.Command, Args: spec.Args}
	start := time.Now().UTC()
	res.StartedAt = start
	finish := func() {
		end := time.Now().UTC()
		res.EndedAt = end
		res.DurationMs = end.Sub(start).Milliseconds()
	}
	infraFail := func(err error) (contract.ExecResult, error) {
		res.Error = err.Error()
		finish()
		return res, nil
	}

	// Resolve the effective workdir under the workspace root.
	workdir, err := securePathUnder(a.cfg.Workdir, a.cfg.Workdir, spec.Workdir)
	if err != nil {
		return infraFail(fmt.Errorf("workdir %q: %w", spec.Workdir, err))
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return infraFail(fmt.Errorf("failed to create workdir %s: %w", workdir, err))
	}

	// Per-exec staging session: stdin materialisation and stdio capture
	// files live in the agent-owned 0700 staging area.
	sessionDir, err := os.MkdirTemp(a.cfg.StagingDir, "exec-")
	if err != nil {
		return infraFail(fmt.Errorf("failed to create staging session dir: %w", err))
	}
	// Reclaim the session on return, after the uploads below have read
	// the capture files. Staging may hold exam-answer content (a stdin
	// blob, captured stdout); leaving it for the next exec in the same
	// run would expose it to same-UID student code under /tmp.
	defer func() { _ = os.RemoveAll(sessionDir) }()

	// Stdin: inline content > workspace path > /dev/null.
	stdinPath := os.DevNull
	stdinProvided := false
	switch {
	case spec.StdinContent != nil:
		stdinPath = filepath.Join(sessionDir, "stdin")
		if err := os.WriteFile(stdinPath, []byte(*spec.StdinContent), 0o600); err != nil {
			return infraFail(fmt.Errorf("failed to materialize stdin: %w", err))
		}
		stdinProvided = true
	case spec.StdinPath != nil:
		p := *spec.StdinPath
		if filepath.IsAbs(p) {
			stdinPath = p
		} else {
			stdinPath, err = securePathUnder(a.cfg.Workdir, a.cfg.Workdir, p)
			if err != nil {
				return infraFail(fmt.Errorf("stdin path %q: %w", p, err))
			}
		}
		stdinProvided = true
	}

	stdoutCapture := filepath.Join(sessionDir, "stdout")
	stderrCapture := filepath.Join(sessionDir, "stderr")

	// Spawn ghost itself as the sandboxed child: `ghost exec`
	// self-applies Landlock/RLIMIT_NPROC and then execve's the
	// command, so the child's exit status IS the command's. The agent
	// process is never sandboxed (Landlock and RLIMIT_NPROC are
	// process-wide and irreversible).
	// Per-file output cap: spec value, else the agent default. Enforced in
	// the child via RLIMIT_FSIZE (kill-and-flag, like the timeout), applied
	// independent of a.cfg.Sandbox: the cap is a resource budget, not a
	// sandbox feature.
	outputLimit := a.cfg.DefaultOutputLimit
	if spec.OutputLimitBytes > 0 {
		outputLimit = spec.OutputLimitBytes
	}
	res.OutputLimitBytes = outputLimit

	args := []string{"exec", "-i", stdinPath, "-o", stdoutCapture, "-e", stderrCapture}
	if a.cfg.Sandbox {
		args = append(args, "--landlock", "--workdir", a.cfg.Workdir)
	}
	if a.cfg.MaxPids > 0 {
		args = append(args, fmt.Sprintf("--max-pids=%d", a.cfg.MaxPids))
	}
	if outputLimit > 0 {
		args = append(args, fmt.Sprintf("--max-file-bytes=%d", outputLimit))
	}
	args = append(args, "--", spec.Command)
	args = append(args, spec.Args...)

	cmd := exec.Command(a.ghostPath(), args...)
	cmd.Dir = workdir
	cmd.Env = buildChildEnv(spec.Env)
	// Pre-execve ghost errors (e.g. flag errors) surface in the agent
	// log; once the child dup3's its fds the command writes to the
	// capture files instead.
	cmd.Stderr = os.Stderr
	// Own process group so a timeout kill reaps the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	timeout := a.cfg.DefaultTimeout
	if spec.TimeoutMs > 0 {
		timeout = time.Duration(spec.TimeoutMs) * time.Millisecond
	}

	if err := cmd.Start(); err != nil {
		// Could not spawn: ExitCode stays null per the contract; no
		// stdio was produced, so nothing is uploaded.
		return infraFail(fmt.Errorf("failed to spawn command: %w", err))
	}

	// Heartbeat while the exec RUNS AND while its captures upload, so core's
	// heartbeat timeout only ever detects a dead container, never a live
	// one still doing post-exec work. Stopped via defer (not inline before
	// the upload phase): a timed-out exec finishes its window with the wire
	// heartbeat cadence already near its edge, and the kill + capture
	// copies + three object-store uploads that follow would otherwise run
	// heartbeat-dark under exactly the starved conditions that delayed the
	// exec (core backend/12).
	hbStop := make(chan struct{})
	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() {
		defer hbWG.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case tick := <-ticker.C:
				// tick carries the SCHEDULED fire time (the 1.23+ timer
				// rework backdates the stamp), so time.Since(tick) is this
				// tick's true servicing delay. Two causes can produce it:
				// runtime CPU starvation (the backend/12 pathology), or the
				// previous RecordHeartbeat blocking this goroutine on a slow
				// wire send (the SDK sends synchronously when no batch
				// window is open). Both are worth a WARN; don't assume
				// which from the message alone. During a multi-tick stall
				// only the oldest buffered tick is delivered, so one WARN
				// may stand for several skipped windows.
				if delay := time.Since(tick); delay > heartbeatTickDelayWarn {
					logger.Warn("heartbeat tick serviced late (runtime CPU starvation or blocked heartbeat send)",
						"delay", delay.String(), "stage", in.Stage, "scenario", in.ScenarioCode)
				}
				activity.RecordHeartbeat(ctx)
			}
		}
	}()
	defer func() {
		close(hbStop)
		hbWG.Wait()
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	var waitErr error
	select {
	case waitErr = <-waitCh:
		timer.Stop()
	case <-timer.C:
		// Deadline exceeded: kill the whole process group. TimedOut is
		// a result, not an error (contract).
		res.TimedOut = true
		killProcessGroup(cmd)
		waitErr = <-waitCh
	case <-ctx.Done():
		// Activity cancelled/timed out by core: kill the child and
		// surface the cancellation as an activity error. (The deferred
		// heartbeat stop runs on return.)
		timer.Stop()
		killProcessGroup(cmd)
		<-waitCh
		finish()
		return contract.ExecResult{}, ctx.Err()
	}
	// The direct child has exited, but its process group may not be empty:
	// a backgrounded descendant, or the survivors of a SIGXFSZ'd writer,
	// would keep running, writing into the captures we are about to copy
	// and upload, and lingering into later execs in this container. Kill
	// the group on every exit path, not just the timeout branch (where
	// this is a harmless repeat).
	killProcessGroup(cmd)
	finish()

	var infraErrs []string
	switch {
	case waitErr == nil:
		code := 0
		res.ExitCode = &code
	case cmd.ProcessState != nil:
		// Includes non-zero exits and signal deaths (-1). A non-zero exit is
		// not an error per the contract.
		code := cmd.ProcessState.ExitCode()
		res.ExitCode = &code
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGXFSZ {
			// The kernel killed the child for breaching RLIMIT_FSIZE: this is
			// the loud half of output-limit detection (the quiet half,
			// capture size, is adjudicated below).
			res.OutputLimitExceeded = true
		}
	default:
		// cmd.Wait() returned no ProcessState. As container init (PID 1) this
		// is the zombie reaper racing cmd.Wait() and winning. Wait4(-1)
		// reaped the child first, so cmd.Wait() saw ECHILD. Recover the real
		// status the reaper captured rather than mis-reporting a spawn/wait
		// failure (which would surface as an "error" scenario).
		if ws, ok := reaper.WaitChild(cmd.Process.Pid); ok && errors.Is(waitErr, syscall.ECHILD) {
			code := ws.ExitStatus() // -1 for signal deaths, matching os/exec
			res.ExitCode = &code
			if ws.Signaled() && ws.Signal() == syscall.SIGXFSZ {
				res.OutputLimitExceeded = true
			}
		} else {
			infraErrs = append(infraErrs, fmt.Sprintf("wait failed: %v", waitErr))
		}
	}

	// Output-limit adjudication, size layer: flag on capture size even when
	// the direct child exited normally. A descendant may have taken the
	// SIGXFSZ (rlimits are inherited), or a handler swallowed the signal and
	// writes failed with EFBIG. A capture of exactly the limit is flagged
	// too: rlimit permits growth to the limit and refuses the byte after,
	// so full-to-the-brim and truncated are indistinguishable. Flag loud
	// and let staff read the captures. (A workdir file a descendant capped
	// is invisible here; the size bound still held for it, only the FLAG is
	// stdio-scoped.)
	if st, err := os.Stat(stdoutCapture); err == nil {
		res.StdoutBytes = st.Size()
	}
	if st, err := os.Stat(stderrCapture); err == nil {
		res.StderrBytes = st.Size()
	}
	if outputLimit > 0 && (res.StdoutBytes >= outputLimit || res.StderrBytes >= outputLimit) {
		res.OutputLimitExceeded = true
	}

	// Copy captures to workdir-relative file destinations for later
	// stages, if requested.
	if spec.StdoutPath != nil {
		if err := copyCapture(stdoutCapture, a.cfg.Workdir, workdir, *spec.StdoutPath); err != nil {
			infraErrs = append(infraErrs, fmt.Sprintf("stdout_path: %v", err))
		}
	}
	if spec.StderrPath != nil {
		if err := copyCapture(stderrCapture, a.cfg.Workdir, workdir, *spec.StderrPath); err != nil {
			infraErrs = append(infraErrs, fmt.Sprintf("stderr_path: %v", err))
		}
	}

	// Stream capture to object storage happens unconditionally:
	// stdout/stderr always (zero-byte objects are fine), stdin iff
	// provided. Upload failures land on Error but the result still
	// carries whatever URIs succeeded. The shared store is the one
	// surface the whole run doesn't own, so uploads are bounded by the
	// output limit even if the RLIMIT_FSIZE chain broke (capForUpload).
	bucket := in.StdioUpload.Bucket
	prefix := in.StdioUpload.KeyPrefix
	for _, s := range []struct {
		name, capture string
		size          int64
		uri           *string
	}{
		{"stdout", stdoutCapture, res.StdoutBytes, &res.StdoutURI},
		{"stderr", stderrCapture, res.StderrBytes, &res.StderrURI},
	} {
		path, capped, err := capForUpload(s.capture, s.size, outputLimit)
		if err != nil {
			infraErrs = append(infraErrs, fmt.Sprintf("cap %s for upload: %v", s.name, err))
			continue
		}
		if capped {
			infraErrs = append(infraErrs, fmt.Sprintf("%s capture exceeded the output limit on disk (enforcement gap) — uploaded the first %d bytes", s.name, outputLimit))
		}
		if err := a.store.UploadFile(ctx, bucket, prefix+"/"+s.name, path); err != nil {
			infraErrs = append(infraErrs, fmt.Sprintf("upload %s: %v", s.name, err))
		} else {
			*s.uri = contract.URIFor(bucket, prefix+"/"+s.name)
		}
	}
	if stdinProvided {
		if err := a.store.UploadFile(ctx, bucket, prefix+"/stdin", stdinPath); err != nil {
			infraErrs = append(infraErrs, fmt.Sprintf("upload stdin: %v", err))
		} else {
			res.StdinURI = contract.URIFor(bucket, prefix+"/stdin")
		}
	}

	res.Error = strings.Join(infraErrs, "; ")
	return res, nil
}

// ghostPath is the binary spawned as the sandboxed child.
func (a *Activities) ghostPath() string {
	if a.cfg.GhostPath != "" {
		return a.cfg.GhostPath
	}
	exe, err := os.Executable()
	if err != nil {
		// Let exec.Command fail with a spawn error carried on the result.
		return "ghost"
	}
	return exe
}

// killProcessGroup SIGKILLs the child's process group (negative pid).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// buildChildEnv builds the explicit child environment: the agent's own
// environment scrubbed of every GHOST_AGENT_* variable (the student
// process must never inherit the Temporal/storage credentials), overlaid
// with the spec's env entries.
func buildChildEnv(overlay map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(overlay))
	index := make(map[string]int)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, agentEnvPrefix) {
			continue
		}
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if i, dup := index[key]; dup {
			out[i] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, key := range slices.Sorted(maps.Keys(overlay)) {
		kv := key + "=" + overlay[key]
		if i, dup := index[key]; dup {
			out[i] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}

// securePathUnder resolves rel against base and rejects any path that
// is absolute or escapes root after cleaning ("" and "." resolve to
// base). base must itself be root or a directory inside it.
func securePathUnder(root, base, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root %s: %w", root, err)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base dir %s: %w", base, err)
	}
	if rel == "" || rel == "." {
		return absBase, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be workspace-relative", rel)
	}
	dest := filepath.Join(absBase, filepath.Clean(rel))
	relCheck, err := filepath.Rel(absRoot, dest)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", rel)
	}
	return dest, nil
}

// capForUpload returns the path to upload for a stdio capture: the capture
// itself when it is within the output limit, else a truncated sibling copy
// (first limit bytes, same staging session dir, reclaimed with it). size >
// limit is unreachable while the child's RLIMIT_FSIZE holds: this is store
// protection against an enforcement gap (flag plumbing dropped, non-Linux
// agent), and the caller records the anomaly on the result Error.
func capForUpload(capturePath string, size, limit int64) (path string, capped bool, err error) {
	if limit <= 0 || size <= limit {
		return capturePath, false, nil
	}
	src, err := os.Open(capturePath)
	if err != nil {
		return "", false, fmt.Errorf("open capture %s: %w", capturePath, err)
	}
	defer func() { _ = src.Close() }()
	cappedPath := capturePath + ".capped"
	dst, err := os.Create(cappedPath)
	if err != nil {
		return "", false, fmt.Errorf("create %s: %w", cappedPath, err)
	}
	_, err = io.CopyN(dst, src, limit)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", false, fmt.Errorf("write %s: %w", cappedPath, err)
	}
	return cappedPath, true, nil
}

// copyCapture copies a stdio capture file to a workdir-relative file
// destination (contract: StdoutPath/StderrPath are file write
// destinations for later stages), refusing workspace escapes.
func copyCapture(capturePath, root, workdir, rel string) (err error) {
	dest, err := securePathUnder(root, workdir, rel)
	if err != nil {
		return err
	}
	if dest == workdir {
		return fmt.Errorf("destination %q is a directory", rel)
	}
	src, err := os.Open(capturePath)
	if err != nil {
		return fmt.Errorf("failed to open capture %s: %w", capturePath, err)
	}
	defer func() { _ = src.Close() }()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", dest, err)
	}
	dst, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}
	return nil
}
