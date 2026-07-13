# Ghost Usage Guide

Comprehensive examples and patterns for using Ghost in various scenarios.

## Table of Contents

- [Command Syntax](#command-syntax)
  - [Exec Command](#exec-command)
  - [Supervise Command](#supervise-command)
  - [Agent Command](#agent-command)
- [Basic Usage](#basic-usage)
  - [Heartbeat (Container Keepalive)](#heartbeat-container-keepalive)
- [Advanced Features](#advanced-features)
  - [Isolation Flags](#isolation-flags)
  - [Sandbox Isolation (legacy run)](#sandbox-isolation-legacy-run)
  - [Supervise Mode](#supervise-mode)
- [Common Use Cases](#common-use-cases)
- [JSON Output Reference](#json-output-reference)
- [Exit Codes](#exit-codes)

## Command Syntax

### Run Command

```
ghost run [flags] -- <command> [args...]
```

The `--` separator is **required** to distinguish Ghost flags from the target command and its arguments. `run` is the legacy command that emits JSON and supports webhooks, uploads, context, and `--score`. For low-overhead or measured execution, use `exec` or `supervise` below.

### Exec Command

```
ghost exec [flags] -- <command> [args...]
```

Replaces the ghost process via `execve` after redirecting stdio. There is no JSON output, webhook, or upload — the command's exit status becomes ghost's. `--landlock` applies Landlock filesystem restrictions. Network isolation is the container/cluster's responsibility (egress NetworkPolicy via `NetworkMode`/`NetworkPolicy`), not ghost's. `--workdir` sets the working directory for Landlock read-write rules, and `--max-pids` caps processes via `RLIMIT_NPROC` (includes ghost itself; 0 = no limit). `--seccomp-profile-json` takes an inline Docker-format seccomp profile JSON that is compiled to a BPF syscall filter and installed before the command runs (empty = no filter; opt-in).

### Supervise Command

```
ghost supervise [flags] -- <command> [args...]
```

Forks the command and keeps ghost alive to measure it (peak memory, OOM attribution, output-size cap), then writes a result trailer to `--result-file` and as a stream frame on stdout. ghost survives the child. It shares `--landlock`, `--workdir`, `--max-pids`, and `--seccomp-profile-json` with `exec`, and adds `--max-output-bytes`, `--result-file`, and `--timeout` (which `exec` does not have — exec replaces the process via `execve`, so it cannot enforce a deadline). Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's. See [Supervise Mode](#supervise-mode).

### Diff Command

```
ghost diff -i <input> -x <expected> -o <output> -e <stderr> [flags]
```

All four I/O flags are required for consistency with the run command.

### Heartbeat Command

```
ghost heartbeat [flags]
```

Runs as PID 1 in a container, writing timestamps for liveness detection and reaping zombie processes.

### Agent Command

```
ghost agent
```

Runs ghost in agent mode (RFD 0015 grading runtime): a long-lived Temporal worker inside a grading container. The agent joins a per-run task queue and serves exactly two activities — `ghost-fetch-submission` (downloads the run's inputs into the workspace) and `ghost-run-exec` (runs one resolved exec spec). Each command is executed in a sandboxed **child** process via `ghost exec --landlock --workdir <wd> --max-pids=N`; the agent process itself is never sandboxed because Landlock and `RLIMIT_NPROC` are process-wide and irreversible. Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's. When the agent is PID 1 it also reaps zombies.

The wire contract (activity names, payload shapes, protocol version) is frozen in `internal/agent/contract`. Agent mode has no flags; everything is configured through `GHOST_AGENT_*` environment variables injected by the runner backend at dispatch. The agent strips **every** `GHOST_AGENT_*` variable from the environment of the commands it spawns, so credentials never reach student code.

| Environment Variable | Required | Default | Description |
|---|---|---|---|
| `GHOST_AGENT_TEMPORAL_ADDRESS` | yes | — | Temporal frontend `host:port` |
| `GHOST_AGENT_TEMPORAL_NAMESPACE` | no | `default` | Temporal namespace |
| `GHOST_AGENT_TASK_QUEUE` | yes | — | Per-run task queue the worker joins |
| `GHOST_AGENT_TEMPORAL_AUTH_TOKEN` | no | empty | Per-run auth token (unused interim; wired in RFD 0015 Phase 8) |
| `GHOST_AGENT_STORAGE_ENDPOINT` | yes | — | S3/MinIO endpoint (`host:port`, or URL whose scheme overrides the secure flag) |
| `GHOST_AGENT_STORAGE_ACCESS_KEY` | yes | — | Object storage access key |
| `GHOST_AGENT_STORAGE_SECRET_KEY` | yes | — | Object storage secret key |
| `GHOST_AGENT_STORAGE_SESSION_TOKEN` | no | empty | STS session token (per-run credentials, Phase 8) |
| `GHOST_AGENT_STORAGE_SECURE` | no | `false` | Use TLS for object storage |
| `GHOST_AGENT_WORKDIR` | no | `/workspace` | Run workspace root; all relative paths in specs resolve against it |
| `GHOST_AGENT_STAGING_DIR` | no | fresh 0700 temp dir | Agent-owned staging area for stdin materialisation and stdio captures (never world-writable) |
| `GHOST_AGENT_DEFAULT_TIMEOUT` | no | `60s` | Exec timeout when a spec's `timeout_ms` is 0 (Go duration) |
| `GHOST_AGENT_MAX_PIDS` | no | `32` | `RLIMIT_NPROC` applied by the child before execve (0 disables) |
| `GHOST_AGENT_SANDBOX` | no | `true` | When true, the child runs with `--landlock` (disable only where the kernel lacks Landlock support, e.g. tests) |
| `GHOST_AGENT_MAX_CONCURRENT_EXECS` | no | `4` | Activities the worker runs at once in this container (0 falls back to the default) |

The first ten names are part of the frozen contract (`contract.Env*` consts); the last five are agent-internal knobs that share the prefix so they are scrubbed alongside the credentials.

```bash
# Typical container entrypoint
export GHOST_AGENT_TEMPORAL_ADDRESS=temporal.internal:7233
export GHOST_AGENT_TASK_QUEUE=ghost-run-55
export GHOST_AGENT_STORAGE_ENDPOINT=minio.internal:9000
export GHOST_AGENT_STORAGE_ACCESS_KEY=...
export GHOST_AGENT_STORAGE_SECRET_KEY=...
ghost agent
```

The agent runs until it receives SIGTERM/SIGINT, then drains in-flight activities gracefully. On a protocol version mismatch with core it fails activities with the non-retryable `GhostProtocolMismatch` error — rebuild the environment image with a current ghost.

## Basic Usage

### Simple Command Execution

```bash
# Execute a command with minimal setup
ghost run -i /dev/null -o output.txt -e error.txt -- echo "Hello, World!"

# Using actual input file
echo "test data" > input.txt
ghost run -i input.txt -o processed.txt -e errors.log -- cat

# Command with arguments
ghost run -i data.csv -o results.json -e stderr.log -- python process.py --format json --validate
```

### File Comparison

```bash
# Basic diff
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e errors.txt

# Diff with scoring (100 if identical, 0 if different)
ghost diff -i submission.txt -x solution.txt -o feedback.txt -e errors.txt --score 100

# Ignore whitespace differences for grading
ghost diff -i student.txt -x answer.txt -o diff.txt -e errors.txt \
  --diff-flags "--ignore-trailing-space --ignore-blank-lines" \
  --score 100
```

### Heartbeat (Container Keepalive)

Run a PID 1 keepalive process that writes Unix timestamps to a file. Intended for use as a container ENTRYPOINT to signal liveness. The heartbeat process also reaps zombie child processes to free PID slots in the cgroup.

```bash
# Default: writes to /output/.heartbeat every 10s
ghost heartbeat

# Custom interval and file
ghost heartbeat --interval 5s --file /tmp/heartbeat
```

Behavior:
- Writes an initial timestamp immediately on start
- Handles SIGTERM and SIGINT for graceful shutdown
- Exits after 5 consecutive write failures
- Reaps zombie children when running as PID 1 (no-op otherwise)

On Linux, when a process dies its parent must call `wait()` to clear it from the process table — otherwise it lingers as a zombie holding a PID slot. Orphaned processes (whose parent died first) get reparented to PID 1, which is responsible for reaping them. In a sandbox container `ghost heartbeat` is PID 1, so it inherits this duty: a `SIGCHLD` handler drains zombies via `Wait4(-1, WNOHANG)`. Without it, fork bomb leftovers would exhaust the cgroup `PidsLimit` and block `docker exec` cleanup commands. This is the same role `tini` and `dumb-init` play.

## Advanced Features

### Isolation Flags

`exec` and `supervise` expose the `--landlock` filesystem isolation and `--seccomp-profile-json` syscall-filtering flags (Linux only). Network isolation is the container/cluster's responsibility (egress NetworkPolicy via `NetworkMode`/`NetworkPolicy`), not ghost's.

- `--landlock` — apply Landlock filesystem restrictions (no namespaces). `--workdir` sets the read-write working directory.
- `--seccomp-profile-json` — apply a seccomp syscall filter. The value is an inline Docker-format seccomp profile JSON (single-sourced from core, never authored by ghost); ghost's pure-Go filter compiles it to a BPF program and installs it via `seccomp(2)` with TSYNC before the command runs, so the command and all descendants inherit it. When empty (the default) no filter is applied — the layer is opt-in.

```bash
# Landlock filesystem restrictions only
ghost exec --landlock --workdir /workspace \
  -i /dev/null -o /output/stdout -e /output/stderr -- python script.py

# Landlock + a process-count cap; this is what the grading agent runs per exec
# spec when GHOST_AGENT_SANDBOX is on (with --max-pids from GHOST_AGENT_MAX_PIDS,
# default 32). Egress is restricted by the container/cluster, not ghost.
ghost exec --landlock --workdir /workspace --max-pids=32 \
  -i /dev/null -o /output/stdout -e /output/stderr -- python3 main.py
```

Landlock filesystem rules:
- **Read-only**: `/usr`, `/bin`, `/lib`, `/lib64`, `/etc`
- **Read-write**: `/output`, `/tmp`, and `--workdir`

### Sandbox Isolation (legacy run)

`ghost run` keeps its original `--sandbox` flag, which applies Landlock filesystem restrictions (Linux only). Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's. Its working directory flag is `--sandbox-workdir`:

```bash
# Basic sandbox — restricts filesystem access
ghost run --sandbox -i /dev/null -o /output/stdout -e /output/stderr -- ./untrusted-command

# Custom working directory for read-write access
ghost run --sandbox --sandbox-workdir /workspace \
  -i /dev/null -o /output/stdout -e /output/stderr -- python script.py
```

`run` does not support `--exec`, `--supervise`, `--landlock`, `--max-pids`, `--max-output-bytes`, or `--result-file` — use `exec` or `supervise` for those.

### Supervise Mode

The `ghost supervise` command is the in-container measurement wrapper for the
multi-backend sandbox. Unlike `ghost exec` (which replaces the ghost process via
`execve` and emits nothing), supervise **forks** the command and keeps ghost
alive alongside it to measure peak memory, attribute OOM kills, enforce the
output-size cap as bytes are written, and emit a structured result trailer when
the command finishes.

```bash
# The core sandbox executor backend runs a no-network container, so it uses
# --landlock only; egress is restricted by the container/cluster
ghost supervise --landlock --max-pids=33 \
  --max-output-bytes=1048576 \
  --result-file=/output/.result \
  -i /dev/null -o /output/stdout -e /output/stderr \
  -- python3 main.py
```

Supervise-specific flags:
- `--max-output-bytes` — total `/output` byte cap (stdout + stderr combined),
  enforced at write time. Default `1048576` (1 MiB). Excess bytes are dropped
  and the trailer's `truncated` flag is set; the child is never killed for
  overshoot.
- `--result-file` — where the result trailer JSON is written. Default
  `/output/.result`.

supervise also accepts `--landlock`, `--workdir`, `--max-pids`, and
`--seccomp-profile-json` (shared with `exec`), plus its own `--timeout` (which
`exec` lacks). It emits no JSON, webhooks, or uploads — only the result trailer
(file + stream frame).

**Child isolation.** `--landlock` applies Landlock filesystem restrictions and
`--seccomp-profile-json` installs a seccomp syscall filter; both are applied to
ghost (the parent) before it forks, so the child inherits them. Network
isolation is the container/cluster's responsibility (egress NetworkPolicy via
`NetworkMode`/`NetworkPolicy`), not ghost's.

**Timeout.** When `--timeout` is set, supervise enforces it by sending
`SIGTERM` to the child's process group, then `SIGKILL` after a grace period. On
its own timeout it still writes the trailer with `exit_code: -1`, so a consumer
always receives a trailer rather than a broken stream.

#### Result trailer

The trailer is a single JSON object, written to two destinations:

1. **A framed line on ghost's own stdout** — the authoritative, forge-proof
   channel. The child's stdout is redirected to `/output/stdout`, so it never
   holds a writable fd to ghost's fd 1 (the exec-attach stream). Even a
   surviving same-UID child fork cannot forge this frame. Backends read it by
   demuxing the exec stream:

   ```
   \x1e\x1eZINC-RESULT\x1e<compact-json>\x1e\x1e
   ```

   The two RS (`0x1e`) bytes plus the `ZINC-RESULT` token guard against false
   matches in the child's real output (which lives in `/output/{stdout,stderr}`,
   never on this stream).
2. **`--result-file`** (e.g. `/output/.result`, mode `0600`) — read directly
   off the bind mount, no orchestrator API call. Kept as a transition fallback:
   a same-UID child fork can rewrite this file, so consumers should prefer the
   stdout frame as the authoritative source and treat the file as a fallback
   only when no frame is present.

Trailer schema (version `1`):

```json
{
  "schema": 1,
  "exit_code": 0,
  "peak_memory_bytes": 5242880,
  "oom_killed": false,
  "truncated": false,
  "duration_ms": 142
}
```

- `exit_code` — the child's wait-status exit code on normal exit; `-1` for a
  timeout or any signalled exit (including an OOM `SIGKILL`).
- `peak_memory_bytes` — sampled from the container's own cgroup v2
  (`memory.current` / `memory.peak`) at the cgroupns root. `0` if cgroup v2 is
  not available (a dev-host degradation; cluster backends require cgroup v2).
- `oom_killed` — true if the cgroup `oom_kill` counter rose during the run.
  This is the authoritative OOM signal, independent of `exit_code`.
- `truncated` — true if output hit `--max-output-bytes`.
- `duration_ms` — wall-clock child runtime.

### Context Metadata

Attach metadata to track execution details:

```bash
# Simple key-value pairs with type inference
ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-kv "job_id=12345" \
  --context-kv "priority=high" \
  --context-kv "retry_count=3" \
  --context-kv "debug=true" \
  -- ./process-job

# Complex nested structures via JSON
ghost run -i data.json -o result.json -e errors.log \
  --context '{
    "pipeline": {
      "stage": "transform",
      "version": "2.1.0"
    },
    "metrics": {
      "input_size": 1024,
      "expected_duration": 300
    }
  }' \
  -- ./etl-pipeline

# Loading from file
cat > metadata.json << EOF
{
  "experiment": {
    "id": "exp-2024-001",
    "parameters": {
      "learning_rate": 0.001,
      "batch_size": 32
    }
  }
}
EOF

ghost run -i dataset.csv -o model.pkl -e training.log \
  --context-file metadata.json \
  -- python train.py

# Combining multiple sources (precedence: kv > json > file > env)
export GHOST_CONTEXT_ENVIRONMENT=production
export GHOST_CONTEXT_REGION=us-east-1

ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-file base-config.json \
  --context '{"override": "from-json"}' \
  --context-kv "override=from-kv" \
  -- ./app
# Result: override will be "from-kv"
```

### Upload to Storage

Upload outputs to MinIO/S3-compatible storage:

```bash
# Using key-value configuration
ghost run -i /dev/null -o results/test-output.txt -e results/test-errors.txt \
  --upload-provider minio \
  --upload-config-kv "endpoint=minio.internal:9000" \
  --upload-config-kv "access_key=$MINIO_ACCESS_KEY" \
  --upload-config-kv "secret_key=$MINIO_SECRET_KEY" \
  --upload-config-kv "bucket=test-results" \
  --upload-config-kv "prefix=$(date +%Y-%m-%d)/" \
  -- ./run-tests.sh

# Using JSON configuration file
cat > s3-config.json << EOF
{
  "endpoint": "s3.amazonaws.com",
  "access_key": "${AWS_ACCESS_KEY_ID}",
  "secret_key": "${AWS_SECRET_ACCESS_KEY}",
  "bucket": "my-app-artifacts",
  "region": "us-west-2",
  "prefix": "builds/",
  "use_ssl": true
}
EOF

ghost run -i /dev/null -o build.log -e build-errors.log \
  --upload-provider minio \
  --upload-config-file s3-config.json \
  -- make build

# Files are uploaded to specified paths after execution completes
```

### Webhook Integration

Send results to external systems with retry logic:

```bash
# Basic webhook notification
ghost run -i test.txt -o result.txt -e error.txt \
  --webhook-url https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXX \
  -- ./critical-process

# With authentication and custom settings
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.monitoring.com/v1/events \
  --webhook-method POST \
  --webhook-auth-type bearer \
  --webhook-auth-token "$API_TOKEN" \
  --webhook-retries 5 \
  --webhook-retry-delay 2s \
  --webhook-timeout 60s \
  -- ./long-running-job

# Webhook with upload and context (complete integration)
ghost run -i batch.csv -o processed.csv -e processing.log \
  --upload-provider minio \
  --upload-config-kv "endpoint=storage.local:9000" \
  --upload-config-kv "bucket=outputs" \
  --webhook-url https://api.pipeline.com/notify \
  --webhook-auth-type api-key \
  --webhook-auth-token "$PIPELINE_API_KEY" \
  --context-kv "batch_id=$(uuidgen)" \
  --context-kv "processor_version=3.2.1" \
  -- python batch_processor.py
```

### Timeout and Verbose Mode

```bash
# Set execution timeout
ghost run -i large-dataset.csv -o analysis.json -e errors.log \
  --timeout 5m \
  -- python heavy_analysis.py

# Verbose mode: see stderr on terminal while also capturing to file
ghost run -i config.yml -o deployment.log -e deployment-errors.log \
  --verbose \
  -- kubectl apply -f config.yml

# Combine timeout with verbose for debugging
ghost run -i test-suite.txt -o test-results.xml -e test-errors.log \
  --timeout 30s \
  --verbose \
  -- npm test
```

## Common Use Cases

### Automated Testing & Grading

```bash
# Grade student submissions with tolerance for formatting
for submission in submissions/*.c; do
  student_id=$(basename "$submission" .c)
  
  # Compile and upload the binary
  ghost run -i /dev/null -o "results/${student_id}_compile.log" \
    -e "results/${student_id}_compile_errors.log" \
    --upload-files "/tmp/${student_id}:binaries/${student_id}" \
    --context-kv "student_id=${student_id}" \
    --context-kv "phase=compilation" \
    --timeout 10s \
    -- gcc -o "/tmp/${student_id}" "$submission"
  
  # Run tests if compilation succeeded
  if [ $? -eq 0 ]; then
    ghost run -i test_input.txt -o "results/${student_id}_output.txt" \
      -e "results/${student_id}_runtime_errors.log" \
      --context-kv "student_id=${student_id}" \
      --context-kv "phase=execution" \
      --timeout 5s \
      -- "/tmp/${student_id}"
    
    # Compare output with expected
    ghost diff -i "results/${student_id}_output.txt" -x expected_output.txt \
      -o "results/${student_id}_diff.txt" -e "results/${student_id}_diff_errors.log" \
      --diff-flags "--ignore-trailing-space --ignore-blank-lines" \
      --score 100 \
      --context-kv "student_id=${student_id}" \
      --context-kv "phase=grading"
  fi
done
```

### CI/CD Pipeline Integration

```bash
#!/bin/bash
# ci-pipeline.sh

# Build stage with artifact upload and local copies
ghost run -i /dev/null -o local/build.log:logs/build.log -e local/errors.log:logs/errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=ci-artifacts" \
  --upload-config-kv "prefix=builds/${BUILD_ID}/" \
  --upload-files "app.exe:binaries/app.exe" \
  --upload-files "app.pdb:debug/app.pdb" \
  --context-kv "stage=build" \
  --context-kv "commit=${GIT_COMMIT}" \
  --context-kv "branch=${GIT_BRANCH}" \
  --timeout 10m \
  --webhook-url "${CI_WEBHOOK_URL}" \
  -- make build

# Test stage (only if build succeeds)
if [ $? -eq 0 ]; then
  ghost run -i /dev/null -o test-results.xml -e test-errors.log \
    --context-kv "stage=test" \
    --context-kv "commit=${GIT_COMMIT}" \
    --score 100 \
    --timeout 15m \
    --webhook-url "${CI_WEBHOOK_URL}" \
    -- make test
fi

# Deploy stage (only if tests pass)
if [ $? -eq 0 ]; then
  ghost run -i /dev/null -o deploy.log -e deploy-errors.log \
    --context-kv "stage=deploy" \
    --context-kv "environment=${DEPLOY_ENV}" \
    --context-kv "version=${VERSION}" \
    --upload-provider minio \
    --upload-config-file deploy-s3.json \
    --webhook-url "${DEPLOY_WEBHOOK_URL}" \
    --webhook-auth-type bearer \
    --webhook-auth-token "${DEPLOY_TOKEN}" \
    -- ./deploy.sh
fi
```

### Data Processing with Additional Files Upload

```bash
# Process data and upload generated reports/artifacts
ghost run -i raw_data.csv -o processing.log -e errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=data-lake" \
  --upload-config-kv "prefix=processed/$(date +%Y%m%d)/" \
  --upload-files "summary_report.pdf" \
  --upload-files "cleaned_data.csv:data/cleaned.csv" \
  --upload-files "visualization.png:images/viz.png" \
  --upload-files "statistics.json:meta/stats.json" \
  -- python process_data.py raw_data.csv

# Compile and upload binary with debug symbols
ghost run -i /dev/null -o compile.log -e compile_errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=builds" \
  --upload-files "program:bin/program" \
  --upload-files "program.map:debug/program.map" \
  --upload-files "program.pdb:debug/program.pdb" \
  -- gcc -g -o program main.c -Wl,-Map=program.map
```

### Performance Benchmarking

```bash
# Run benchmarks and collect timing data
for size in 100 1000 10000 100000; do
  ghost run -i "data_${size}.json" -o "bench_${size}.out" -e "bench_${size}.err" \
    --context-kv "input_size=${size}" \
    --context-kv "algorithm=quicksort" \
    --context-kv "timestamp=$(date -Iseconds)" \
    --timeout 1m \
    -- ./benchmark --size "${size}"
done

# Parse execution times from JSON output
for size in 100 1000 10000 100000; do
  echo "Size ${size}: $(ghost run ... | jq -r '.execution_time')ms"
done
```

### Batch Processing with Notifications

```bash
# Process multiple files with webhook notifications
find ./incoming -name "*.xml" | while read -r file; do
  basename_no_ext=$(basename "$file" .xml)
  
  ghost run -i "$file" -o "processed/${basename_no_ext}.json" \
    -e "errors/${basename_no_ext}.log" \
    --context-kv "source_file=${file}" \
    --context-kv "process_time=$(date -Iseconds)" \
    --upload-provider minio \
    --upload-config-kv "endpoint=storage:9000" \
    --upload-config-kv "bucket=processed-data" \
    --webhook-url https://api.monitoring.com/batch-status \
    --webhook-auth-type api-key \
    --webhook-auth-token "${MONITORING_API_KEY}" \
    -- python xml_to_json.py
    
  # Move processed file
  [ $? -eq 0 ] && mv "$file" ./completed/
done
```

## JSON Output Reference

### Standard Output Structure

```json
{
  "command": "echo Hello World",           // Always present
  "status": "success",                     // success | failed | timeout
  "input": "/dev/null",                    // Always present
  "output": "output.txt",                  // Always present
  "stderr": "stderr.txt",                  // Always present
  "exit_code": 0,                         // -1 for timeout
  "execution_time": 125,                  // Milliseconds
  "timeout": 30000,                       // Only if --timeout used
  "score": 85,                            // Only if --score used
  "context": {                            // Only if context provided
    "user_id": 123,
    "test_case": "integration_01"
  },
  "webhook_sent": true,                   // Only if webhook configured
  "webhook_error": ""                     // Empty on success
}
```

### Diff Command Output

```json
{
  "command": "diff actual.txt expected.txt",
  "status": "failed",                     // failed = files differ
  "input": "actual.txt",
  "expected": "expected.txt",             // Only in diff output
  "output": "diff.txt",
  "stderr": "errors.txt",
  "exit_code": 1,                        // 0 = identical, 1 = different
  "execution_time": 8,
  "score": 0                              // 0 because files differ
}
```

### Parsing Output Examples

```bash
# Extract exit code
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command | jq -r '.exit_code'

# Check if command succeeded
if [ "$(ghost run ... | jq -r '.status')" = "success" ]; then
  echo "Command succeeded"
fi

# Get execution time in seconds
ghost run ... | jq -r '.execution_time / 1000'

# Extract context data
ghost run ... | jq -r '.context.user_id'

# Check webhook status
ghost run ... | jq -r 'if .webhook_sent then "Webhook sent" else "Webhook failed: " + .webhook_error end'
```

## Exit Codes

Ghost itself uses the following exit codes:

- **0**: Ghost executed successfully (target command exit code is in JSON)
- **1**: Ghost encountered an error (invalid flags, file access issues, etc.)
- **2**: Invalid command usage (missing required flags, no command specified)

The target command's exit code is captured in the JSON output's `exit_code` field.

## Tips and Best Practices

1. **Always use absolute paths** when running Ghost in scripts to avoid path resolution issues

2. **Check Ghost's exit code first**, then parse JSON for the target command's result:
   ```bash
   output=$(ghost run -i in.txt -o out.txt -e err.txt -- ./cmd)
   if [ $? -ne 0 ]; then
     echo "Ghost failed to execute"
     exit 1
   fi
   
   exit_code=$(echo "$output" | jq -r '.exit_code')
   if [ "$exit_code" -ne 0 ]; then
     echo "Command failed with exit code: $exit_code"
   fi
   ```

3. **Use context for debugging** - include relevant metadata that helps troubleshoot issues:
   ```bash
   --context-kv "hostname=$(hostname)" \
   --context-kv "user=$USER" \
   --context-kv "pwd=$(pwd)"
   ```

4. **Set appropriate timeouts** for long-running commands to prevent hanging:
   ```bash
   --timeout 5m  # Generous timeout for compilation
   --timeout 30s # Reasonable timeout for tests
   ```

5. **Use verbose mode during development**, disable in production:
   ```bash
   [ "$DEBUG" = "true" ] && VERBOSE_FLAG="-v" || VERBOSE_FLAG=""
   ghost run -i in.txt -o out.txt -e err.txt $VERBOSE_FLAG -- ./cmd
   ```

6. **Store webhook credentials securely** using environment variables or secret management:
   ```bash
   export GHOST_WEBHOOK_AUTH_TOKEN=$(vault read -field=token secret/webhook)
   ```

7. **Use diff flags for grading** to ignore insignificant differences:
   ```bash
   --diff-flags "--ignore-all-space --ignore-blank-lines"
   ```

8. **Batch webhook notifications** to avoid overwhelming endpoints during bulk operations

9. **Test upload configuration** with small files before processing large datasets

10. **Parse JSON output safely** using proper tools like `jq` instead of regex

## See Also

- [Configuration Reference](CONFIG.md) - Complete list of flags and environment variables
- [README](README.md) - Quick start guide
- [Developer Notes](CLAUDE.md) - Implementation details and development guidance
