# Ghost

A Go CLI command runner for orchestrating command execution with structured output and metadata capture.

## Overview

Ghost is a command orchestration tool that executes commands while capturing execution metadata including exit codes, execution time, and optional scoring. It provides structured JSON output making it ideal for automation, testing, and CI/CD pipelines.

## Why "Ghost"?

The name draws inspiration from several playful concepts:

- **Ghost in the Shell**: Like the famous anime/manga, Ghost operates at the system level, orchestrating processes and capturing their essence (output) while remaining transparent to the underlying commands.

- **Racing Ghost**: Similar to the ghost racers in Mario Kart that record and replay performances, Ghost captures command outputs and redirects them to different locations, creating a "recording" of your command execution.

- **Silent Observer**: Ghost watches your commands execute without interfering—it observes, captures, and reports, like a friendly specter documenting everything that happens during execution.

In essence, Ghost is your ethereal assistant that seamlessly captures and redirects I/O streams while remaining nearly invisible to the processes it monitors.

## Installation

```bash
go install github.com/zinc-sig/ghost@latest
```

Or build from source:

```bash
git clone https://github.com/zinc-sig/ghost.git
cd ghost
go build -o ghost
```

## Quick Start

### Run a Command

Execute any command with required I/O redirection:

```bash
# Basic execution
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command arg1 arg2

# With scoring (returns 0 if command fails)
ghost run -i /dev/null -o output.txt -e stderr.txt --score 100 -- python script.py

# With timeout
ghost run -i input.txt -o output.txt -e stderr.txt --timeout 30s -- ./slow-command
```

### Exec a Command

`ghost exec` replaces the ghost process via `execve` after redirecting stdio. There is no JSON output, webhook, or upload — the command's exit status becomes ghost's. The `--landlock` flag applies filesystem isolation and `--seccomp-profile-json` applies a seccomp syscall filter (an inline Docker-format profile, single-sourced from core; empty = no filter). Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's.

```bash
# Basic exec — zero overhead, no JSON output
ghost exec -i /dev/null -o /output/stdout -e /output/stderr -- ./command

# Landlock filesystem restrictions only
ghost exec --landlock --workdir /workspace \
  -i /dev/null -o /output/stdout -e /output/stderr -- python script.py

# Landlock + a process-count cap; this is what the grading agent uses for
# each exec spec
ghost exec --landlock --workdir /workspace --max-pids=33 \
  -i /dev/null -o /output/stdout -e /output/stderr -- python3 main.py
```

### Supervise a Command

`ghost supervise` forks the command and keeps ghost alive to measure peak memory, attribute OOM kills, enforce an output-size cap at write time, and emit a result trailer to a result file and as a stream frame on stdout. ghost survives the child. The measurement wrapper for the multi-backend sandbox. See [USAGE.md](USAGE.md#supervise-mode).

```bash
# The core sandbox executor backend runs a no-network container, so it relies on
# --landlock (filesystem) plus --seccomp-profile-json (syscall filtering) for
# in-container isolation; egress is restricted by the container/cluster.
# $SECCOMP_JSON holds the inline Docker-format profile core single-sources.
ghost supervise --landlock --seccomp-profile-json="$SECCOMP_JSON" \
  --max-pids=33 --max-output-bytes=1048576 \
  --result-file=/output/.result \
  -i /dev/null -o /output/stdout -e /output/stderr -- python3 main.py
```

### Sandbox Isolation (legacy `run`)

`ghost run` keeps its original combined `--sandbox` flag, which applies Landlock filesystem restrictions (Linux only). Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's.

```bash
# Restricts filesystem access
ghost run --sandbox -i /dev/null -o /output/stdout -e /output/stderr -- ./untrusted-command

# Custom working directory for read-write access
ghost run --sandbox --sandbox-workdir /workspace -i /dev/null -o /output/stdout -e /output/stderr -- python script.py
```

For the `--landlock` flag, use `exec` or `supervise` instead.

### Heartbeat (Container Keepalive)

Run a PID 1 keepalive process that writes Unix timestamps to a file. Intended for use as a container ENTRYPOINT:

```bash
# Default: writes to /output/.heartbeat every 10s
ghost heartbeat

# Custom interval and file
ghost heartbeat --interval 5s --file /tmp/heartbeat
```

### Compare Files

Use the diff command for structured file comparison:

```bash
# Basic diff
ghost diff -i actual.txt -x expected.txt -o diff.txt -e stderr.txt

# With scoring and whitespace tolerance
ghost diff -i student.txt -x solution.txt -o diff.txt -e stderr.txt \
  --score 100 --diff-flags "--ignore-trailing-space"
```

### Webhook Integration

Send execution results to external systems:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/results \
  --webhook-auth-type bearer \
  --webhook-auth-token "your-token" \
  -- ./my-command
```

### Upload to Storage

Upload output files to MinIO/S3:

```bash
ghost run -i /dev/null -o results/output.txt -e results/stderr.txt \
  --upload-provider minio \
  --upload-config-kv "endpoint=localhost:9000" \
  --upload-config-kv "bucket=results" \
  --upload-config-kv "access_key=admin" \
  --upload-config-kv "secret_key=password" \
  -- echo "Hello World"
```

## JSON Output

Ghost outputs structured JSON to stdout:

```json
{
  "command": "echo Hello World",
  "status": "success",
  "input": "/dev/null",
  "output": "output.txt",
  "stderr": "stderr.txt",
  "exit_code": 0,
  "execution_time": 12,
  "score": 100
}
```

## Key Features

- ✅ **Required I/O redirection** - All commands must specify input, output, and stderr files
- 📊 **Structured JSON output** - Machine-readable execution metadata
- ⏱️ **Execution timing** - Precise millisecond measurements
- 🎯 **Optional scoring** - Conditional score based on exit code
- 📤 **Upload support** - Send outputs to MinIO/S3 storage
- 🔔 **Webhook integration** - Notify external systems with results
- 🔄 **Retry logic** - Configurable retries for webhooks
- 📝 **Context metadata** - Attach arbitrary JSON data to executions
- 🔍 **File comparison** - Built-in diff with structured output
- ⏳ **Timeout support** - Automatic process termination
- 🔧 **Environment configuration** - Configure via environment variables
- 🔒 **Isolation** - `--landlock` (filesystem) and `--seccomp-profile-json` (syscall filtering) flags on `exec`/`supervise`; `--sandbox` (Landlock) on legacy `run` (Linux). Network isolation is the container/cluster's responsibility (egress NetworkPolicy), not ghost's
- 🛡️ **Process limiting** - RLIMIT_NPROC enforcement to prevent fork bombs (`--max-pids` on `exec`/`supervise`)
- 🚀 **Exec mode** - Zero-overhead process replacement via `syscall.Exec` (`ghost exec`)
- 📏 **Supervise mode** - Fork+measure with peak memory, OOM attribution, output cap, and a result trailer (`ghost supervise`)
- 💓 **Heartbeat** - PID 1 container keepalive with timestamp file

## Documentation

- 📖 **[Full Usage Guide](USAGE.md)** - Comprehensive examples and use cases
- ⚙️ **[Configuration Reference](CONFIG.md)** - All flags and environment variables

## License

MIT License - see LICENSE file for details.
