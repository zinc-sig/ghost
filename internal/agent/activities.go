package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// sampler is shared by every running exec: one goroutine enforces all
	// memory budgets and sweeps escaped processes between execs.
	sampler *memorySampler
}

// NewActivities builds the activity implementations from the agent
// config and an object store (a fake in tests).
func NewActivities(cfg *Config, store ObjectStore) *Activities {
	return &Activities{cfg: cfg, store: store, sampler: newMemorySampler()}
}

// workspaceObjectsVersion is the protocol version that introduced
// FetchSubmissionInput.Objects and Answer. An older input carrying either
// was built by a core whose contract copy disagrees with its version, so
// it is a mismatch rather than a payload to serve.
const workspaceObjectsVersion = 3

// checkProtocol fails the activity with a non-retryable ApplicationError
// of the contract's mismatch type when the version is outside the range
// the agent serves: retrying cannot fix a stale environment image.
func checkProtocol(version int) error {
	if version < contract.MinProtocolVersion || version > contract.ProtocolVersion {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("agent serves protocol v%d to v%d, core sent v%d: rebuild the environment image",
				contract.MinProtocolVersion, contract.ProtocolVersion, version),
			contract.ProtocolMismatchErrorType,
			nil,
		)
	}
	return nil
}

// FetchSubmission stages the run workspace and completes the
// readiness/version handshake (contract: ghost-fetch-submission). Every
// attempt stages from scratch in the contract's order: the prefix
// downloads, the answer set aside, the teacher objects, the answer placed.
// Only a teacher object standing in the answer's way fails with the
// contract's staging-invalid type; the package README describes the rules.
func (a *Activities) FetchSubmission(ctx context.Context, in contract.FetchSubmissionInput) (contract.FetchSubmissionResult, error) {
	if err := checkProtocol(in.ProtocolVersion); err != nil {
		return contract.FetchSubmissionResult{}, err
	}
	if in.ProtocolVersion < workspaceObjectsVersion && (len(in.Objects) > 0 || in.Answer != nil) {
		return contract.FetchSubmissionResult{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("workspace objects and the answer placement require protocol v%d, core sent v%d",
				workspaceObjectsVersion, in.ProtocolVersion),
			contract.ProtocolMismatchErrorType,
			nil,
		)
	}

	logger := activity.GetLogger(ctx)
	res := contract.FetchSubmissionResult{
		AgentProtocolVersion: in.ProtocolVersion,
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

	// The answer leaves the workspace before any object is written, so a
	// teacher object at its delivered name cannot replace it. It is found
	// among the files this attempt downloaded; a retry never reuses the
	// copy an earlier attempt set aside, because that session is gone.
	var answer *stagedAnswer
	if in.Answer != nil {
		session, err := os.MkdirTemp(a.cfg.StagingDir, "answer-")
		if err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("failed to create staging session dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(session) }()
		answer, err = setAsideAnswer(a.cfg.Workdir, session, in.Answer.Stem)
		if err != nil {
			return contract.FetchSubmissionResult{}, err
		}
		if answer != nil && len(answer.others) > 0 {
			logger.Warn("fetch-submission: several root files match the answer stem; the first by name is the answer",
				"stem", in.Answer.Stem, "answer", answer.name, "others", answer.others)
		}
	}

	// Teacher objects overwrite whatever the downloads left at their
	// targets. The written paths are kept for the answer's clash check.
	var written []string
	for _, o := range in.Objects {
		dests := make([]string, 0, len(o.TargetPaths))
		for _, rel := range o.TargetPaths {
			dest, err := placeFile(a.cfg.Workdir, rel, 0o755)
			if err != nil {
				return contract.FetchSubmissionResult{}, fmt.Errorf("object target %q: %w", rel, err)
			}
			dests = append(dests, dest)
		}
		if len(dests) == 0 {
			continue
		}
		n, err := a.store.DownloadObject(ctx, o.Bucket, o.Key, dests, 0o644)
		if err != nil {
			return contract.FetchSubmissionResult{}, err
		}
		res.Files += len(dests)
		res.Bytes += n * int64(len(dests))
		written = append(written, dests...)
		activity.RecordHeartbeat(ctx)
	}

	// The answer is placed last and wins its target. A delivered file or
	// directory in the way is removed; a teacher object in the way is the
	// one staging failure that no rerun can fix.
	if answer != nil {
		rel := in.Answer.TargetPath
		if rel == "" {
			rel = answer.name
		}
		dest, err := resolveFile(a.cfg.Workdir, rel)
		if err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("answer target %q: %w", rel, err)
		}
		if err := objectBlocks(a.cfg.Workdir, dest, written); err != nil {
			return contract.FetchSubmissionResult{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("answer %s cannot be placed at %s: %v", answer.name, rel, err),
				contract.StagingInvalidErrorType,
				nil,
			)
		}
		if _, err := placeFile(a.cfg.Workdir, rel, 0o755); err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("answer target %q: %w", rel, err)
		}
		if err := moveFile(answer.path, dest); err != nil {
			return contract.FetchSubmissionResult{}, fmt.Errorf("failed to place answer %s at %s: %w", answer.name, rel, err)
		}
		logger.Info("fetch-submission: answer placed", "stem", in.Answer.Stem, "answer", answer.name, "target", rel)
	}
	return res, nil
}

// stagedAnswer is the student's answer file after it was set aside in the
// staging session: its delivered root name, where it waits, and the other
// root files that matched the stem and stay in place.
type stagedAnswer struct {
	name   string
	path   string
	others []string
}

// answerCandidates returns the names of the regular files directly under
// root whose name minus its extension equals stem, in byte-wise name
// order. A name without an extension matches as a whole.
func answerCandidates(root, stem string) ([]string, error) {
	if stem == "" {
		return nil, errors.New("answer stem is empty")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace root %s: %w", root, err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.TrimSuffix(name, filepath.Ext(name)) == stem {
			names = append(names, name)
		}
	}
	return names, nil
}

// setAsideAnswer moves the first candidate for stem out of root into the
// staging session. It returns nil when no root file matches, which leaves
// a teacher stub at the target in place.
func setAsideAnswer(root, session, stem string) (*stagedAnswer, error) {
	names, err := answerCandidates(root, stem)
	if err != nil || len(names) == 0 {
		return nil, err
	}
	staged := filepath.Join(session, names[0])
	if err := moveFile(filepath.Join(root, names[0]), staged); err != nil {
		return nil, fmt.Errorf("failed to set aside answer %s: %w", names[0], err)
	}
	return &stagedAnswer{name: names[0], path: staged, others: names[1:]}, nil
}

// resolveFile resolves a workspace-relative file path under root and
// rejects the root itself, which no file write can target.
func resolveFile(root, rel string) (string, error) {
	dest, err := securePathUnder(root, root, rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root %s: %w", root, err)
	}
	if dest == absRoot {
		return "", fmt.Errorf("path %q is the workspace root", rel)
	}
	return dest, nil
}

// placeFile prepares dest for a file write under root and returns it: a
// non-directory on the way (a delivered file where a directory must be)
// and anything at the target that is not a regular file are removed, and
// missing parents are created with dirMode. The caller creates or
// truncates the file itself, so a regular file at the target is replaced
// by the write rather than removed here.
func placeFile(root, rel string, dirMode os.FileMode) (string, error) {
	dest, err := resolveFile(root, rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root %s: %w", root, err)
	}
	relPath, err := filepath.Rel(absRoot, dest)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", rel, err)
	}
	parts := strings.Split(relPath, string(filepath.Separator))
	dir := absRoot
	for _, part := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, part)
		st, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to inspect %s: %w", dir, err)
		}
		if st.IsDir() {
			continue
		}
		// Nothing can exist below a file or link, so removing it clears
		// the rest of the path for MkdirAll.
		if err := os.Remove(dir); err != nil {
			return "", fmt.Errorf("failed to remove %s: %w", dir, err)
		}
		break
	}
	if st, err := os.Lstat(dest); err == nil && !st.Mode().IsRegular() {
		if err := os.RemoveAll(dest); err != nil {
			return "", fmt.Errorf("failed to remove %s: %w", dest, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return "", fmt.Errorf("failed to create directory for %s: %w", dest, err)
	}
	return dest, nil
}

// objectBlocks reports a teacher object standing in the answer's way at
// dest: a written file below dest means an object made a directory there,
// and a written file that is a strict ancestor of dest sits where a parent
// directory must be. A written path equal to dest is not a block, because
// the answer replaces it. Only paths that still exist as regular files
// count, so an object a later object replaced does not.
func objectBlocks(root, dest string, written []string) error {
	sep := string(filepath.Separator)
	for _, p := range written {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		switch {
		case strings.HasPrefix(p, dest+sep):
			return fmt.Errorf("an object wrote %s below the target, which is therefore a directory", workspaceRel(root, p))
		case strings.HasPrefix(dest, p+sep):
			return fmt.Errorf("an object wrote a file at %s, which the target needs as a directory", workspaceRel(root, p))
		}
	}
	return nil
}

// workspaceRel renders an absolute workspace path relative to root for
// error messages, falling back to the path itself.
func workspaceRel(root, p string) string {
	if absRoot, err := filepath.Abs(root); err == nil {
		if rel, err := filepath.Rel(absRoot, p); err == nil {
			return rel
		}
	}
	return p
}

// moveFile renames src to dst, replacing a regular file at dst. When the
// two are on different filesystems, as the workspace volume and the
// staging directory usually are, it copies with the source's permission
// bits and then removes the source.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// copyFile copies src to dst, creating or truncating dst with the
// source's permission bits.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
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

	// Memory budget: the spec value enforced on the process group by the
	// sampler, echoed on the result. 0 means no per-exec enforcement and
	// selects no agent default, because core sizes the container cap from
	// the budgets it sent; the contract comment on MemoryLimitBytes states
	// the reason.
	memoryLimit := spec.MemoryLimitBytes
	if memoryLimit < 0 {
		memoryLimit = 0
	}
	res.MemoryLimitBytes = memoryLimit

	// --oom-victim makes a kernel kill at the container cap land in the
	// command tree rather than the agent; it is applied regardless of
	// a.cfg.Sandbox because it protects the agent, not the workspace.
	args := []string{"exec", "-i", stdinPath, "-o", stdoutCapture, "-e", stderrCapture, "--oom-victim"}
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

	// Start and register with the sampler in one step so the new process
	// group is never visible to a sibling exec's orphan sweep before it is
	// registered.
	watch, err := a.sampler.startWatched(cmd, memoryLimit)
	if err != nil {
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
		a.sampler.finish(watch)
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

	// Unregister from the sampler after the group is dead, so its final
	// sample can attribute a kernel kill in the last tick and its sweep can
	// remove any process that escaped the group. MemoryLimitExceeded is a
	// result, not an error, like TimedOut.
	mem := a.sampler.finish(watch)
	res.MemoryLimitExceeded = mem.exceeded
	res.PeakMemoryBytes = mem.peak
	if mem.exceeded {
		logger.Info("run-exec memory limit exceeded", "stage", in.Stage, "scenario", in.ScenarioCode,
			"killed_by", mem.reason, "limit_bytes", memoryLimit, "peak_bytes", mem.peak)
	}
	if len(mem.swept) > 0 {
		logger.Warn("run-exec orphan sweep killed processes outside every running exec's group",
			"stage", in.Stage, "scenario", in.ScenarioCode, "pids", mem.swept)
	}

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
