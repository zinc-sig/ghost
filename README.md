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

### Run Command

Execute a command with required input/output redirection:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command my_args
```

Execute with scoring (all I/O flags are required):

```bash
ghost run -i input.txt -o output.txt -e stderr.txt --score 85 -- python script.py
```

### Upload Support

Ghost supports uploading output files to remote storage providers like MinIO/S3. When upload is configured, files are written to temporary locations during execution and then uploaded to the specified remote paths.

Upload to MinIO with configuration:

```bash
ghost run -i /dev/null -o results/output.txt -e results/stderr.txt \
  --upload-provider minio \
  --upload-config-kv "endpoint=localhost:9000" \
  --upload-config-kv "access_key=minioadmin" \
  --upload-config-kv "secret_key=minioadmin" \
  --upload-config-kv "bucket=ghost-results" \
  --upload-config-kv "prefix=tests/" \
  -- echo "Hello World"
```

Using environment variables for upload configuration:

```bash
export GHOST_UPLOAD_CONFIG_ENDPOINT=localhost:9000
export GHOST_UPLOAD_CONFIG_ACCESS_KEY=minioadmin
export GHOST_UPLOAD_CONFIG_SECRET_KEY=minioadmin
export GHOST_UPLOAD_CONFIG_BUCKET=ghost-results
export GHOST_UPLOAD_CONFIG_PREFIX=tests/

ghost run -i /dev/null -o output.txt -e stderr.txt \
  --upload-provider minio \
  -- echo "Hello World"
```

Using JSON configuration:

```bash
ghost run -i /dev/null -o output.txt -e stderr.txt \
  --upload-provider minio \
  --upload-config '{
    "endpoint": "localhost:9000",
    "access_key": "minioadmin",
    "secret_key": "minioadmin",
    "bucket": "ghost-results",
    "prefix": "tests/"
  }' \
  -- echo "Hello World"
```

### Webhook Support

Ghost can send execution results to webhooks for integration with external systems:

```bash
# Basic webhook
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/results \
  -- ./my-command

# With authentication (Bearer token)
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/results \
  --webhook-auth-type bearer \
  --webhook-auth-token "your-token-here" \
  -- ./my-command

# With API key authentication
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/results \
  --webhook-auth-type api-key \
  --webhook-auth-token "your-api-key" \
  -- ./my-command

# With retry configuration
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/results \
  --webhook-retries 5 \
  --webhook-retry-delay 2s \
  --webhook-timeout 60s \
  -- ./my-command
```

Using environment variables for webhook configuration:

```bash
export GHOST_WEBHOOK_URL=https://api.example.com/results
export GHOST_WEBHOOK_AUTH_TYPE=bearer
export GHOST_WEBHOOK_AUTH_TOKEN=your-token-here
export GHOST_WEBHOOK_RETRIES=3
export GHOST_WEBHOOK_RETRY_DELAY=1s
export GHOST_WEBHOOK_TIMEOUT=30s

ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command
```

Webhook with upload and context:

```bash
ghost run -i /dev/null -o output.txt -e stderr.txt \
  --upload-provider minio \
  --upload-config-kv "endpoint=localhost:9000" \
  --upload-config-kv "bucket=results" \
  --webhook-url https://api.example.com/notify \
  --context-kv "job_id=12345" \
  --context-kv "user=alice" \
  -- ./process-data
```

With timeout and verbose output:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt --timeout 30s --verbose -- ./slow-command
```

With context metadata:

```bash
# Using key-value pairs (with automatic type inference)
ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-kv "student_id=s123" \
  --context-kv "assignment=hw1" \
  --context-kv "max_score=100" \
  --context-kv "strict_mode=true" \
  -- ./student_program

# Using JSON string for complex structures
ghost run -i input.txt -o output.txt -e stderr.txt \
  --context '{"metadata": {"version": 2, "timestamp": "2024-01-01"}}' \
  -- ./my-command

# Using context file
ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-file metadata.json \
  -- ./my-command

# Using environment variables
GHOST_CONTEXT='{"batch": "2024"}' \
GHOST_CONTEXT_USER_ID=123 \
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command
```

### Diff Command

Compare two files and get structured output:

```bash
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e stderr.txt
```

Compare with scoring (score applies if files match):

```bash
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e stderr.txt --score 100
```

With custom diff flags for grading:

```bash
ghost diff -i student.txt -x solution.txt -o diff.txt -e stderr.txt --diff-flags "--ignore-trailing-space"
```

With context for tracking test metadata:

```bash
ghost diff -i actual.txt -x expected.txt -o diff.txt -e stderr.txt \
  --context-kv "test_case=5" \
  --context-kv "suite=integration" \
  --score 100
```

With webhook notification:

```bash
ghost diff -i actual.txt -x expected.txt -o diff.txt -e stderr.txt \
  --webhook-url https://api.example.com/grading \
  --webhook-auth-type bearer \
  --webhook-auth-token "token-here" \
  --score 100
```

## JSON Output

Ghost outputs execution results as JSON to stdout:

```json
{
  "command": "echo hello world",
  "status": "success",
  "input": "input.txt",
  "output": "output.txt", 
  "stderr": "stderr.txt",
  "exit_code": 0,
  "execution_time": 590,
  "timeout": 5000,
  "score": 85,
  "context": {
    "student_id": "s123",
    "assignment": "hw1",
    "max_score": 100,
    "strict_mode": true
  },
  "webhook_sent": true,
  "webhook_error": ""
}
```

For diff commands, includes the expected field:

```json
{
  "command": "diff file1.txt file2.txt",
  "status": "failed",
  "input": "file1.txt",
  "expected": "file2.txt",
  "output": "diff_output.txt",
  "stderr": "stderr.txt",
  "exit_code": 1,
  "execution_time": 12
}
```

### Field Descriptions

- **command**: The executed command as a string
- **status**: Execution status ("success", "failed", or "timeout")
- **input/output/stderr**: File paths for I/O redirection (required)
- **expected**: File path for expected output (diff command only)
- **exit_code**: Process exit code (0 for success, -1 for timeout)
- **execution_time**: Command execution time in milliseconds
- **timeout**: Timeout duration in milliseconds (optional)
- **score**: Integer score value (optional, 0 if command fails)
- **context**: Arbitrary JSON data for metadata (optional)
- **webhook_sent**: Boolean indicating if webhook was sent successfully (optional, only when webhook is configured)
- **webhook_error**: Error message if webhook failed (optional, empty string on success)

**Note**: The `-i`, `-o`, and `-e` flags are mandatory for all command executions.

## Features

- **I/O Redirection**: Required redirection of stdin, stdout, and stderr to files
- **File Comparison**: Built-in diff command for comparing files with structured output
- **Execution Timing**: Precise execution time measurement in milliseconds
- **Timeout Support**: Set execution time limits with automatic process termination
- **Status Tracking**: Clear status indicators (success/failed/timeout)
- **Verbose Mode**: Optionally display stderr on terminal while capturing to file
- **Command Logging**: Full command string included in JSON output for auditing
- **Score Tracking**: Optional scoring with conditional logic
- **Context Metadata**: Attach arbitrary JSON data to command executions via multiple input methods
- **Type Inference**: Automatic detection of numbers and booleans in key-value pairs
- **Upload Support**: Upload output files to MinIO/S3-compatible storage providers
- **Webhook Integration**: Send results to webhooks with authentication and retry support
- **Environment Configuration**: Configure upload and webhook settings via environment variables
- **Diff Flags**: Pass custom flags to diff for flexible comparison (e.g., ignore whitespace)
- **Structured Output**: JSON format for easy parsing and automation
- **Exit Code Capture**: Reliable exit code reporting
- **Auto Directory Creation**: Parent directories are created automatically for output files
- **POSIX Compliant**: Built with Cobra framework for professional CLI experience

## Context Support

Ghost allows you to attach arbitrary metadata to command executions through the context field. This is useful for tracking test cases, user information, execution environments, or any other metadata relevant to your use case.

### Input Methods

1. **Key-Value Pairs** (`--context-kv`): Simple key=value format with automatic type inference
   ```bash
   --context-kv "user_id=123" --context-kv "enabled=true" --context-kv "score=95.5"
   ```

2. **JSON String** (`--context`): For complex nested structures
   ```bash
   --context '{"metadata": {"version": 2, "tags": ["test", "integration"]}}'
   ```

3. **File** (`--context-file`): Load context from a JSON file
   ```bash
   --context-file metadata.json
   ```

4. **Environment Variables**: Set context through environment
   ```bash
   GHOST_CONTEXT='{"env": "production"}'  # JSON object
   GHOST_CONTEXT_USER_ID=123               # Individual keys (lowercased)
   GHOST_CONTEXT_DEBUG=true                # With type inference
   ```

### Precedence Rules

When the same key appears in multiple sources, the precedence order is:
1. Key-value pairs (highest priority)
2. JSON string flag
3. Context file
4. Environment variables (lowest priority)

### Type Inference

When using `--context-kv` or `GHOST_CONTEXT_*` environment variables, Ghost automatically infers types:
- Numbers: `"123"` → `123`, `"3.14"` → `3.14`
- Booleans: `"true"` → `true`, `"false"` → `false`
- Strings: Everything else remains as strings

## Use Cases

- **Testing Frameworks**: Execute tests with structured result capture
- **CI/CD Pipelines**: Orchestrate build and deployment commands
- **Performance Monitoring**: Track command execution times
- **Process Automation**: Standardized command execution with logging
- **Score-based Systems**: Educational or competitive programming platforms

## License

MIT License - see LICENSE file for details.