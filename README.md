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

## Documentation

- 📖 **[Full Usage Guide](USAGE.md)** - Comprehensive examples and use cases
- ⚙️ **[Configuration Reference](CONFIG.md)** - All flags and environment variables
- 🤖 **[Developer Notes](CLAUDE.md)** - Claude Code guidance and project structure

## Why "Ghost"?

Like a ghost in the shell, Ghost operates transparently at the system level—observing, capturing, and reporting command execution while remaining nearly invisible to the processes it monitors.

## License

MIT License - see LICENSE file for details.
